#!/bin/bash
# The 9-config UI QA matrix, in parallel: one simulator per config (iPhone 17 Pro Max, iPhone SE,
# iPad landscape, iPad portrait; each light and dark; plus iPhone Duo in light). Target: under 30
# minutes. Build 118 (Xcode 27): the iPhones and the Duo run iOS 27.2, the iPads stay on iOS 26.4,
# so both the new and the previous iOS are covered. runs_on below says where each test runs.
#
#   scripts/qa-matrix.sh                      # every config, every test
#   CONFIGS="ipadL-light" TESTS="UnsplashPhotoQATests" scripts/qa-matrix.sh   # a targeted rerun
#   RERUN=/path/to/old/summary.txt scripts/qa-matrix.sh   # only what failed there, nothing else
#   PLAN_ONLY=1 scripts/qa-matrix.sh          # which simulator runs which test; runs nothing
#
# Needs two local APIs on local Mongo (docs/development.md): 8082 with no photo search key, and
# 8083 with UNSPLASH_ACCESS_KEY=qa-not-a-real-key pointed at cmd/fakeunsplash. Start both with
# REFERRAL_MAX_PER_30_DAYS=1000 so parallel referral runs don't use up the sharer's slots, and
# AUTH_RATE_BURST=1000 so eight simulators signing in at once aren't told to wait. Start 8082 with
# REVIEW_ACCOUNT_EMAIL and REVIEW_ACCOUNT_PASSWORD too, and export the same two here: they reach
# ReviewAccountQATests as TEST_RUNNER_QA_REVIEW_*. Without them that test is left out.
# 8083 also gets STARTER_RECIPES_HOUSEHOLD_ID from scripts/qa-starter-library.sh, so the review
# path test sees a starter library in a brand-new household.
#
# The app is built once per API port (build-for-testing), then every config runs
# test-without-building at the same time. Each test has a 10-minute limit, and a run with no
# output for 10 minutes is killed, its simulator restarted, and that test retried once.
# Results: $OUT/summary.txt, one line per config and test; $OUT/<config>-<test>.xcresult.
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${OUT:-${TMPDIR:-/tmp}/dinneros-qa}
ALL="large-light large-dark small-light small-dark ipadL-light ipadL-dark ipadP-light ipadP-dark duo-light"
CONFIGS=${CONFIGS:-$ALL}
TESTS=${TESTS:-"AppSweepQATests/SWEEP ScreensQATests/testDeeperScreens ScreensQATests/testNewPersonFirstRun AnyListGuideQATests ReferralQATests InviteLinkQATests UnsplashPhotoQATests AddRecipeChooserQATests ImportFailureQATests DiscoverQATests AddRecipeQATests SaveThenOpenQATests CompanionQATests CuisineQATests NextStepDotQATests ReportRecipeQATests ReviewPathQATests AccountDeletionQATests PermissionsDeniedQATests ReviewAccountQATests BiometricSignInQATests/testFakeFaceID BiometricSignInQATests/testSimulatorFaceID BiometricSignInQATests/testFaceIDSignInLoop BiometricSignInQATests/testSignInScreenWithFaceID StartCookingQATests"}
STALL=${STALL:-600}
# Tests to run on a second simulator per config, "QA matrix <config> 2", at the same time as the
# first. Off by default: on this Mac (build 118) 17 simulators ran 40 to 70% slower each and
# filled the disk. Set it (space-separated, with a space at each end) on a bigger machine.
FLOWS=${FLOWS:-" "}
# RERUN: a summary.txt from an earlier run. Only its FAILED config and test pairs run again.
RERUN=${RERUN:-}
PAIRS=""
if [ -n "$RERUN" ]; then
    PAIRS=$(grep -E ": FAILED" "$RERUN" | sed -E 's/^([^ ]+) ([^:]+):.*/\1|\2/' |
        sed -E 's#AppSweepQATests/testSweep(Light|Dark)#AppSweepQATests/SWEEP#')
    [ -n "$PAIRS" ] || { echo "Nothing failed in $RERUN"; exit 0; }
    CONFIGS=$(echo "$PAIRS" | cut -d'|' -f1 | sort -u | tr '\n' ' ')
    TESTS=$(echo "$PAIRS" | cut -d'|' -f2 | sort -u | tr '\n' ' ')
fi
RUNTIME_PHONE=${RUNTIME_PHONE:-com.apple.CoreSimulator.SimRuntime.iOS-27-2}
RUNTIME_IPAD=${RUNTIME_IPAD:-com.apple.CoreSimulator.SimRuntime.iOS-26-4}
# iPhone Duo only runs on its own iOS 27.2 runtime.
RUNTIME_DUO=${RUNTIME_DUO:-com.apple.CoreSimulator.SimRuntime.iOS-27-2-iPhone19-4}
mkdir -p "$OUT"
: >"$OUT/summary.txt"
rm -f "$OUT/STOP"

device_type() {
    case $1 in
    large-*) echo com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro-Max ;;
    small-*) echo com.apple.CoreSimulator.SimDeviceType.iPhone-SE-3rd-generation ;;
    ipad*) echo com.apple.CoreSimulator.SimDeviceType.iPad-Pro-11-inch-M5-12GB ;;
    duo-*) echo com.apple.CoreSimulator.SimDeviceType.iPhone-Duo ;;
    esac
}

# One simulator per config and shard, named "QA matrix <config>" (shard 1) or
# "QA matrix <config> 2", made once and reused.
simulator() {
    local name="QA matrix $1" id
    [ "${2:-1}" = 2 ] && name="$name 2"
    id=$(xcrun simctl list devices -j | python3 -c "
import json,sys
for devs in json.load(sys.stdin)['devices'].values():
    for d in devs:
        if d['name']=='$name' and d['isAvailable']: print(d['udid']); raise SystemExit")
    if [ -z "$id" ]; then
        local rt=$RUNTIME_PHONE
        case $1 in ipad*) rt=$RUNTIME_IPAD ;; duo-*) rt=$RUNTIME_DUO ;; esac
        id=$(xcrun simctl create "$name" "$(device_type "$1")" "$rt")
    fi
    echo "$id"
}

build() {
    local port=$1
    xcodebuild build-for-testing -project "$ROOT/ios/DinnerOS.xcodeproj" -scheme "DinnerOS QA" \
        -destination "generic/platform=iOS Simulator" -derivedDataPath "$OUT/dd-$port" \
        "API_BASE_URL=http:/\$()/localhost:$port" ARCHS=arm64 CODE_SIGN_IDENTITY=- >"$OUT/build-$port.log" 2>&1 ||
        { echo "build for $port failed: $OUT/build-$port.log"; exit 1; }
}

# Runs one test on one simulator, killing it after $STALL seconds without output.
run_test() {
    local config=$1 id=$2 test=$3 attempt=$4
    local mode=${config#*-} orientation=portrait port=8082 search=off
    case $config in ipadL-*) orientation=landscape ;; esac
    # Where Start Cooking belongs: the bottom bar on iPad and the Duo's cover screen (build 119).
    local cook=top
    case $config in ipad* | duo*) cook=bottom ;; esac
    test=${test/SWEEP/$([ "$mode" = dark ] && echo testSweepDark || echo testSweepLight)}
    [ "$test" = UnsplashPhotoQATests ] && { port=8083; search=on; }
    # The review path needs a starter library, which only 8083 copies into new households.
    [ "$test" = ReviewPathQATests ] && port=8083
    local name="$config-$(echo "$test" | tr '/' '-')"
    local xctestrun
    xctestrun=$(ls "$OUT/dd-$port"/Build/Products/*.xctestrun | head -1)
    xcrun simctl uninstall "$id" com.linesmerrill.dinneros 2>/dev/null
    # The build is signed (so the Keychain works, which Face ID sign-in needs), and a simulator's
    # Keychain outlives an uninstall: clear it so every test starts signed out.
    xcrun simctl keychain "$id" reset 2>/dev/null
    rm -rf "$OUT/$name.xcresult"
    TEST_RUNNER_QA_API_BASE=http://localhost:$port TEST_RUNNER_QA_PHOTO_SEARCH=$search TEST_RUNNER_QA_MODE=$mode TEST_RUNNER_QA_ORIENTATION=$orientation \
        TEST_RUNNER_QA_APP_QA_HUD_SECONDS=3 TEST_RUNNER_QA_APP_QA_MOMENT_SECONDS=12 TEST_RUNNER_QA_MANUAL_NAME="Sip Fudge" \
        TEST_RUNNER_QA_IMPORTED_NAME="Street Cart-Style Chicken Bowls" TEST_RUNNER_QA_EXPECT_START_COOKING=$cook \
        TEST_RUNNER_QA_REVIEW_EMAIL="${REVIEW_ACCOUNT_EMAIL:-}" TEST_RUNNER_QA_REVIEW_PASSWORD="${REVIEW_ACCOUNT_PASSWORD:-}" \
        xcodebuild test-without-building -xctestrun "$xctestrun" -destination "id=$id" \
        -test-timeouts-enabled YES -maximum-test-execution-time-allowance 600 \
        -resultBundlePath "$OUT/$name.xcresult" -only-testing:"DinnerOSUITests/$test" \
        >"$OUT/$name.log" 2>&1 &
    local pid=$!
    while kill -0 $pid 2>/dev/null; do
        sleep 15
        local idle=$(($(date +%s) - $(stat -f %m "$OUT/$name.log")))
        if [ $idle -gt "$STALL" ]; then
            kill $pid 2>/dev/null
            pkill -f "id=$id" 2>/dev/null
            xcrun simctl shutdown "$id" 2>/dev/null
            xcrun simctl boot "$id" 2>/dev/null
            xcrun simctl bootstatus "$id" -b >/dev/null 2>&1
            xcrun simctl ui "$id" appearance "$mode"
            if [ "$attempt" = 1 ]; then
                echo "$config $test: stalled ${STALL}s, restarted" >>"$OUT/summary.txt"
                run_test "$config" "$id" "$3" 2
            else
                echo "$config $test: FAILED (stalled twice)" >>"$OUT/summary.txt"
            fi
            return
        fi
    done
    # A fresh simulator can refuse the first install ("Internal error"): retry that once.
    if [ "$attempt" = 1 ] && grep -q "failed to install the application\|Unable to Install" "$OUT/$name.log"; then
        echo "$config $test: install failed, retried" >>"$OUT/summary.txt"
        sleep 10
        run_test "$config" "$id" "$3" 2
        return
    fi
    local passed failed
    passed=$(grep -cE "Test Case.*passed" "$OUT/$name.log")
    failed=$(grep -cE "Test Case.*failed" "$OUT/$name.log")
    local verdict=ok
    [ "$failed" -gt 0 ] || [ "$passed" -eq 0 ] && verdict=FAILED
    echo "$config $test: $verdict ($passed passed, $failed failed)" >>"$OUT/summary.txt"
    grep -E " error:" "$OUT/$name.log" | sed "s|$ROOT/||" | cut -c1-300 | head -3 >>"$OUT/summary.txt"
    # Disk: a passing flow test's result bundle (a screen recording) isn't needed. The sweeps and
    # screens keep theirs for screenshot QA, and every failure keeps its own.
    # Screenshot tests keep their results; other passing tests don't, to save disk.
    case "$verdict $test" in
    "ok AppSweepQATests/"* | "ok ScreensQATests/"* | "ok StartCookingQATests" | "ok BiometricSignInQATests/testSignInScreenWithFaceID") ;;
    "ok "*) rm -rf "$OUT/$name.xcresult" ;;
    esac
}

# Whether a test runs on a config: the placement rules for every test.
runs_on() {
    local config=$1 test=$2
    # The Duo (build 118) checks how screens look on its size, not the long flows.
    if [ "${config%%-*}" = duo ]; then
        case $test in
        AppSweepQATests/SWEEP | ScreensQATests/* | ImportFailureQATests | DiscoverQATests) return 0 ;;
        StartCookingQATests | BiometricSignInQATests/testSignInScreenWithFaceID) return 0 ;;
        BiometricSignInQATests/testFaceIDSignInLoop) return 0 ;;
        *) return 1 ;;
        esac
    fi
    # Build 118 (Xcode 27, iOS 27.2): every test ran 10 to 15% slower, and the first full run hit
    # 30 minutes before any config finished. A second simulator per config ran everything 40 to 70%
    # slower again and filled the disk, so the work itself shrank instead:
    # - Sweeps and both screen tests still run on every config: they are how each screen looks.
    # - The short tests that ran on all 8 run on 3 or 4, at least one iPhone and one iPad each.
    # - The long flows (referral, invite link, AnyList) check what a person does, so each runs on
    #   one iPhone and one iPad.
    # Then each device's tests are split between light and dark so every config finishes in about
    # 28 minutes. PLAN_ONLY=1 prints the result.
    case $test in
    AppSweepQATests/SWEEP | ScreensQATests/*) return 0 ;;
    # Build 119: the sign-in screen with Face ID kept is a screen to check everywhere. Start
    # Cooking pinned to the bottom (the Duo's cover screen) is checked on the Duo and, where it
    # must not move, on an iPhone of each size and both iPads.
    BiometricSignInQATests/testSignInScreenWithFaceID) return 0 ;;
    StartCookingQATests) case $config in large-light | small-dark | ipadL-light | ipadP-dark) ;; *) return 1 ;; esac ;;
    ReferralQATests) case $config in large-light | ipadL-light) ;; *) return 1 ;; esac ;;
    InviteLinkQATests) case $config in small-light | ipadP-dark) ;; *) return 1 ;; esac ;;
    AnyListGuideQATests) case $config in large-dark | ipadL-dark) ;; *) return 1 ;; esac ;;
    UnsplashPhotoQATests) case $config in large-light | small-dark | ipadL-dark) ;; *) return 1 ;; esac ;;
    AddRecipeChooserQATests) case $config in large-light | small-dark | ipadL-light) ;; *) return 1 ;; esac ;;
    ImportFailureQATests) case $config in large-dark | small-light | ipadL-light) ;; *) return 1 ;; esac ;;
    DiscoverQATests) case $config in large-light | small-dark | ipadP-light | ipadP-dark) ;; *) return 1 ;; esac ;;
    # The end-to-end add tests run once each: adding by hand on the large iPhone, companions and
    # cuisine on a small iPhone and on the iPad on its side (where their forms are a sheet).
    AddRecipeQATests | SaveThenOpenQATests) [ "$config" = large-dark ] || return 1 ;;
    CompanionQATests) case $config in small-light | ipadL-dark) ;; *) return 1 ;; esac ;;
    CuisineQATests) case $config in small-dark | ipadL-dark) ;; *) return 1 ;; esac ;;
    NextStepDotQATests) case $config in large-dark | ipadP-light) ;; *) return 1 ;; esac ;;
    ReportRecipeQATests) case $config in small-dark | ipadL-light) ;; *) return 1 ;; esac ;;
    # Pre-submission checks (decisions 741, 749): the review path on one iPhone and one iPad,
    # account deletion and denied permissions once each.
    ReviewPathQATests) case $config in small-dark | ipadP-dark) ;; *) return 1 ;; esac ;;
    AccountDeletionQATests) [ "$config" = ipadP-light ] || return 1 ;;
    PermissionsDeniedQATests) [ "$config" = small-dark ] || return 1 ;;
    # Face ID sign-in (decision 760): the long fake-biometrics flow (about 5 minutes) on the upright
    # iPad, and the simulator's own Face ID (about 1.5 minutes) on the small iPhone.
    BiometricSignInQATests/testFakeFaceID) [ "$config" = ipadP-light ] || return 1 ;;
    BiometricSignInQATests/testSimulatorFaceID) [ "$config" = small-light ] || return 1 ;;
    # The whole Face ID sign-in loop (build 119, about 5 minutes) on one iPhone and one iPad: the
    # Duo and the upright iPad in light had the most time left (on large-light and ipadL-dark the
    # first full run took 31.7 and 31.5 minutes).
    BiometricSignInQATests/testFaceIDSignInLoop) case $config in duo-light | ipadP-light) ;; *) return 1 ;; esac ;;
    # The App Review account (decision 752) is one account, so its test runs once, and only
    # when the review account's email and password are exported for it.
    ReviewAccountQATests)
        [ "$config" = ipadL-dark ] || return 1
        if [ -z "${REVIEW_ACCOUNT_EMAIL:-}" ] || [ -z "${REVIEW_ACCOUNT_PASSWORD:-}" ]; then
            echo "$config $test: skipped (export REVIEW_ACCOUNT_EMAIL and REVIEW_ACCOUNT_PASSWORD)" >>"$OUT/summary.txt"
            return 1
        fi ;;
    esac
    return 0
}

run_config() {
    local config=$1 shard=$2 id
    id=$(simulator "$config" "$shard")
    xcrun simctl boot "$id" 2>/dev/null
    xcrun simctl bootstatus "$id" -b >/dev/null 2>&1
    xcrun simctl ui "$id" appearance "${config#*-}"
    for test in $TESTS; do
        [ -f "$OUT/STOP" ] && break
        if [ -n "$PAIRS" ]; then echo "$PAIRS" | grep -qxF "$config|$test" || continue; fi
        # Shard 2 runs the long flows, shard 1 everything else.
        case "$FLOWS" in *" $test "*) [ "$shard" = 2 ] || continue ;; *) [ "$shard" = 1 ] || continue ;; esac
        runs_on "$config" "$test" || continue
        run_test "$config" "$id" "$test" 1
    done
    xcrun simctl shutdown "$id"
}

# PLAN_ONLY=1: print which simulator runs which test, and run nothing.
if [ "${PLAN_ONLY:-}" = 1 ]; then
    for config in $CONFIGS; do
        for shard in 1 2; do
            for test in $TESTS; do
                if [ -n "$PAIRS" ]; then echo "$PAIRS" | grep -qxF "$config|$test" || continue; fi
                case "$FLOWS" in *" $test "*) [ "$shard" = 2 ] || continue ;; *) [ "$shard" = 1 ] || continue ;; esac
                [ "$test" = ReviewAccountQATests ] && [ -z "${REVIEW_ACCOUNT_EMAIL:-}" ] && continue
                runs_on "$config" "$test" && echo "$config/$shard $test"
            done
        done
    done
    exit 0
fi
start=$(date +%s)
# Simulators, installs and result bundles need room; a full disk shows up as failed installs and
# a crashed local Mongo, not as a clear error.
# Simulators grow during a run, and a full disk crashed local Mongo in build 118's first
# two-simulator run. Erasing the simulators first (ERASE=1) frees most of it.
if [ "${ERASE:-}" = 1 ]; then
    xcrun simctl list devices | grep -E "QA matrix " | grep -oE "[0-9A-F-]{36}" | while read -r sid; do
        xcrun simctl shutdown "$sid" 2>/dev/null
        xcrun simctl erase "$sid"
    done
fi
free_gb=$(df -g "$OUT" | awk 'NR==2 {print $4}')
# About 3 GB per simulator plus the builds: 31 GB for the full matrix, less for a rerun.
need_gb=$((3 * $(echo $CONFIGS | wc -w) + 4))
if [ "$free_gb" -lt "$need_gb" ]; then
    echo "Only ${free_gb} GB free; this run needs ${need_gb}. Clear old result bundles, or run with ERASE=1."
    exit 1
fi
need8083=no
for t in $TESTS; do case $t in UnsplashPhotoQATests | ReviewPathQATests) need8083=yes ;; esac; done
# Each invite that counts takes a free month code from the local pool (decision 755). Seed made-up
# ones through the development-only route, so the referral tests end at "Your free month is ready".
seed_offer_codes() {
    local port=$1 token
    token=$(curl -fsS -X POST "http://localhost:$port/api/v1/auth/dev" -H 'Content-Type: application/json' \
        -d '{"subject":"qa-matrix-offer-codes"}' | python3 -c 'import json,sys; print(json.load(sys.stdin)["accessToken"])') &&
        curl -fsS -o /dev/null -X POST "http://localhost:$port/api/v1/plus/dev/offer-codes" \
            -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d '{"count":200}' ||
        { echo "couldn't seed free month codes on $port: is that API running with developer sign-in on?"; exit 1; }
}
seed_offer_codes 8082
[ $need8083 = yes ] && seed_offer_codes 8083
build 8082 &
[ $need8083 = yes ] && build 8083 &
wait
echo "built in $(($(date +%s) - start))s" >>"$OUT/summary.txt"
# A full disk crashed local Mongo once (build 118). Below 5 GB free, stop every test and say so.
(
    while sleep 10; do
        if [ "$(df -g "$OUT" | awk 'NR==2 {print $4}')" -lt 5 ]; then
            echo "STOPPED: under 5 GB free; tests killed" >>"$OUT/summary.txt"
            touch "$OUT/STOP"
            pkill -f "test-without-building.*$OUT" 2>/dev/null
            exit 0
        fi
    done
) &
disk_watch=$!
# Staggered a little, so the simulators don't boot and install at the same instant. A second
# simulator only starts for a config that has a flow test to run.
has_flow() {
    local config=$1 test
    for test in $TESTS; do
        case "$FLOWS" in *" $test "*) ;; *) continue ;; esac
        if [ -n "$PAIRS" ]; then echo "$PAIRS" | grep -qxF "$config|$test" || continue; fi
        runs_on "$config" "$test" && return 0
    done
    return 1
}
for config in $CONFIGS; do
    run_config "$config" 1 &
    sleep 5
    if has_flow "$config"; then
        run_config "$config" 2 &
        sleep 5
    fi
done
wait $(jobs -p | grep -vx "$disk_watch")
kill "$disk_watch" 2>/dev/null
wait "$disk_watch" 2>/dev/null
echo "done in $(($(date +%s) - start))s" >>"$OUT/summary.txt"
cat "$OUT/summary.txt"
grep -qE "FAILED|STOPPED" "$OUT/summary.txt" && exit 1
exit 0
