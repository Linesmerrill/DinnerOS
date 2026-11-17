#!/bin/bash
# The 8-config UI QA matrix, in parallel: one simulator per config (iPhone 17 Pro Max, iPhone SE,
# iPad landscape, iPad portrait; each light and dark). Target: under 30 minutes.
#
#   scripts/qa-matrix.sh                      # every config, every test
#   CONFIGS="ipadL-light" TESTS="UnsplashPhotoQATests" scripts/qa-matrix.sh   # a targeted rerun
#   RERUN=/path/to/old/summary.txt scripts/qa-matrix.sh   # only what failed there, nothing else
#
# Needs two local APIs on local Mongo (docs/development.md): 8082 with no photo search key, and
# 8083 with UNSPLASH_ACCESS_KEY=qa-not-a-real-key pointed at cmd/fakeunsplash. Start both with
# REFERRAL_MAX_PER_30_DAYS=1000 so parallel referral runs don't use up the sharer's slots, and
# AUTH_RATE_BURST=1000 so eight simulators signing in at once aren't told to wait.
#
# The app is built once per API port (build-for-testing), then every config runs
# test-without-building at the same time. Each test has a 10-minute limit, and a run with no
# output for 10 minutes is killed, its simulator restarted, and that test retried once.
# Results: $OUT/summary.txt, one line per config and test; $OUT/<config>-<test>.xcresult.
set -u
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${OUT:-${TMPDIR:-/tmp}/dinneros-qa}
ALL="large-light large-dark small-light small-dark ipadL-light ipadL-dark ipadP-light ipadP-dark"
CONFIGS=${CONFIGS:-$ALL}
TESTS=${TESTS:-"AppSweepQATests/SWEEP ScreensQATests/testDeeperScreens ScreensQATests/testNewPersonFirstRun AnyListGuideQATests ReferralQATests InviteLinkQATests UnsplashPhotoQATests AddRecipeChooserQATests ImportFailureQATests DiscoverQATests AddRecipeQATests SaveThenOpenQATests CompanionQATests CuisineQATests NextStepDotQATests ReportRecipeQATests ReviewPathQATests AccountDeletionQATests PermissionsDeniedQATests"}
STALL=${STALL:-600}
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
RUNTIME_PHONE=${RUNTIME_PHONE:-com.apple.CoreSimulator.SimRuntime.iOS-26-2}
RUNTIME_IPAD=${RUNTIME_IPAD:-com.apple.CoreSimulator.SimRuntime.iOS-26-4}
mkdir -p "$OUT"
: >"$OUT/summary.txt"

device_type() {
    case $1 in
    large-*) echo com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro-Max ;;
    small-*) echo com.apple.CoreSimulator.SimDeviceType.iPhone-SE-3rd-generation ;;
    ipad*) echo com.apple.CoreSimulator.SimDeviceType.iPad-Pro-11-inch-M5-12GB ;;
    esac
}

# One simulator per config, named "QA matrix <config>", made once and reused.
simulator() {
    local name="QA matrix $1" id
    id=$(xcrun simctl list devices -j | python3 -c "
import json,sys
for devs in json.load(sys.stdin)['devices'].values():
    for d in devs:
        if d['name']=='$name' and d['isAvailable']: print(d['udid']); raise SystemExit")
    if [ -z "$id" ]; then
        local rt=$RUNTIME_PHONE
        case $1 in ipad*) rt=$RUNTIME_IPAD ;; esac
        id=$(xcrun simctl create "$name" "$(device_type "$1")" "$rt")
    fi
    echo "$id"
}

build() {
    local port=$1
    xcodebuild build-for-testing -project "$ROOT/ios/DinnerOS.xcodeproj" -scheme "DinnerOS QA" \
        -destination "generic/platform=iOS Simulator" -derivedDataPath "$OUT/dd-$port" \
        "API_BASE_URL=http:/\$()/localhost:$port" ARCHS=arm64 CODE_SIGNING_ALLOWED=NO >"$OUT/build-$port.log" 2>&1 ||
        { echo "build for $port failed: $OUT/build-$port.log"; exit 1; }
}

# Runs one test on one simulator, killing it after $STALL seconds without output.
run_test() {
    local config=$1 id=$2 test=$3 attempt=$4
    local mode=${config#*-} orientation=portrait port=8082 search=off
    case $config in ipadL-*) orientation=landscape ;; esac
    test=${test/SWEEP/$([ "$mode" = dark ] && echo testSweepDark || echo testSweepLight)}
    [ "$test" = UnsplashPhotoQATests ] && { port=8083; search=on; }
    local name="$config-$(echo "$test" | tr '/' '-')"
    local xctestrun
    xctestrun=$(ls "$OUT/dd-$port"/Build/Products/*.xctestrun | head -1)
    xcrun simctl uninstall "$id" com.linesmerrill.dinneros 2>/dev/null
    rm -rf "$OUT/$name.xcresult"
    TEST_RUNNER_QA_API_BASE=http://localhost:$port TEST_RUNNER_QA_PHOTO_SEARCH=$search TEST_RUNNER_QA_MODE=$mode TEST_RUNNER_QA_ORIENTATION=$orientation \
        TEST_RUNNER_QA_APP_QA_HUD_SECONDS=3 TEST_RUNNER_QA_APP_QA_MOMENT_SECONDS=12 TEST_RUNNER_QA_MANUAL_NAME="Sip Fudge" \
        TEST_RUNNER_QA_IMPORTED_NAME="Street Cart-Style Chicken Bowls" \
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
}

run_config() {
    local config=$1 id
    id=$(simulator "$config")
    xcrun simctl boot "$id" 2>/dev/null
    xcrun simctl bootstatus "$id" -b >/dev/null 2>&1
    xcrun simctl ui "$id" appearance "${config#*-}"
    for test in $TESTS; do
        if [ -n "$PAIRS" ]; then echo "$PAIRS" | grep -qxF "$config|$test" || continue; fi
        # The end-to-end add tests run once each, on configs without the referral and invite
        # tests: adding by hand on the large iPhone, companions and cuisine on the small iPhone and
        # on the iPad on its side (where their forms are a sheet), and the next-step dots (about a
        # minute) on the large iPhone and the upright iPad, the two lightest configs.
        # ReferralQATests also covers the free month moments ("Step 2 done" after its recipe, then
        # "Your free month is ready" with Redeem), so they run on the same 4 configs at no extra cost.
        # The long flow tests (referral, invite link, AnyList) check what a person does, not how a
        # screen looks, so each runs on one appearance per device: 4 configs, not 8. Screens and
        # sweeps still run on all 8. The two groups are split so every config finishes in about the
        # same time. The failed-import test (about a minute, nothing saved) checks how a screen
        # looks, so it runs on all 8. Reporting a shared recipe and hiding its cook (about a minute)
        # checks what a person does, so it runs on the small iPhone and the iPad on its side, the
        # configs without the next-step dots.
        case $test in
        ReferralQATests | InviteLinkQATests)
            case $config in large-light | small-dark | ipadL-light | ipadP-dark) ;; *) continue ;; esac ;;
        AnyListGuideQATests)
            case $config in large-dark | small-light | ipadL-dark | ipadP-light) ;; *) continue ;; esac ;;
        AddRecipeQATests | SaveThenOpenQATests) [ "$config" = large-dark ] || continue ;;
        CompanionQATests | CuisineQATests) case $config in small-light | ipadL-dark) ;; *) continue ;; esac ;;
        NextStepDotQATests) case $config in large-dark | ipadP-light) ;; *) continue ;; esac ;;
        ReportRecipeQATests) case $config in small-dark | ipadL-light) ;; *) continue ;; esac ;;
        # Pre-submission checks (decisions 741, 749): the review path on one iPhone and one iPad,
        # account deletion and denied permissions once each, on configs with time to spare.
        ReviewPathQATests) case $config in large-light | ipadP-dark) ;; *) continue ;; esac ;;
        AccountDeletionQATests) [ "$config" = large-dark ] || continue ;;
        PermissionsDeniedQATests) [ "$config" = small-light ] || continue ;;
        esac
        run_test "$config" "$id" "$test" 1
    done
    xcrun simctl shutdown "$id"
}

start=$(date +%s)
# Simulators, installs and result bundles need room; a full disk shows up as failed installs and
# a crashed local Mongo, not as a clear error.
free_gb=$(df -g "$OUT" | awk 'NR==2 {print $4}')
if [ "$free_gb" -lt 10 ]; then
    echo "Only ${free_gb} GB free; the matrix needs 10. Clear old result bundles first."
    exit 1
fi
need8083=no
for t in $TESTS; do [ "$t" = UnsplashPhotoQATests ] && need8083=yes; done
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
# Staggered a little, so 8 simulators don't boot and install at the same instant.
for config in $CONFIGS; do
    run_config "$config" &
    sleep 15
done
wait
echo "done in $(($(date +%s) - start))s" >>"$OUT/summary.txt"
cat "$OUT/summary.txt"
grep -q "FAILED" "$OUT/summary.txt" && exit 1
exit 0
