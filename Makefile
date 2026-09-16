# DinnerOS developer commands. Run `make help` for a summary.

SHELL := /bin/bash
API_DIR := api
IOS_DIR := ios
IOS_SCHEME := DinnerOS
IOS_DESTINATION ?= platform=iOS Simulator,name=iPhone 17 Pro,OS=latest
STATICCHECK_VERSION := 2025.1.1

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available commands
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# --- Local services -----------------------------------------------------------

.PHONY: mongo-up mongo-down
mongo-up: ## Start local MongoDB (docker compose)
	docker compose up -d mongo

mongo-down: ## Stop local MongoDB
	docker compose down

# --- API ----------------------------------------------------------------------

.PHONY: api-run api-test api-test-integration api-lint api-build api-fmt
api-run: ## Run the API locally (loads .env if present)
	@set -a; [ -f .env ] && source .env; set +a; go -C $(API_DIR) run ./cmd/server

api-test: ## Run API unit tests (MongoDB integration tests skip)
	go -C $(API_DIR) test ./...

api-test-integration: ## Run API tests including MongoDB integration tests (needs `make mongo-up`)
	MONGODB_TEST_URI=$${MONGODB_TEST_URI:-mongodb://localhost:27017} go -C $(API_DIR) test -race -count=1 ./...

api-lint: ## gofmt check, go vet, staticcheck
	@unformatted="$$(gofmt -l $(API_DIR))"; if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	go -C $(API_DIR) vet ./...
	go -C $(API_DIR) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

api-fmt: ## Format API code
	gofmt -w $(API_DIR)

api-build: ## Build the API binary into api/bin/
	go -C $(API_DIR) build -o bin/server ./cmd/server

# --- iOS ----------------------------------------------------------------------

.PHONY: ios-build ios-test ios-lint ios-fmt
ios-build: ## Build the iOS app for the simulator
	# pipefail is set inline, not via .SHELLFLAGS: macOS ships GNU Make 3.81, which
	# ignores it. Without it a failed build whose output xcbeautify formatted fine
	# would exit 0.
	set -o pipefail; xcodebuild -project $(IOS_DIR)/DinnerOS.xcodeproj -scheme $(IOS_SCHEME) -destination '$(IOS_DESTINATION)' build CODE_SIGNING_ALLOWED=NO | xcbeautify 2>/dev/null || \
	xcodebuild -project $(IOS_DIR)/DinnerOS.xcodeproj -scheme $(IOS_SCHEME) -destination '$(IOS_DESTINATION)' -quiet build CODE_SIGNING_ALLOWED=NO

ios-test: ## Run iOS unit tests on the simulator
	# No -quiet: it suppresses the "** TEST SUCCEEDED **" banner, so a passing run
	# and a failing one look identical to anything reading the output.
	xcodebuild -project $(IOS_DIR)/DinnerOS.xcodeproj -scheme $(IOS_SCHEME) -destination '$(IOS_DESTINATION)' test CODE_SIGNING_ALLOWED=NO

ios-lint: ## Lint Swift sources with swift-format (bundled with Xcode)
	xcrun swift-format lint --strict --recursive --configuration $(IOS_DIR)/.swift-format $(IOS_DIR)/DinnerOS $(IOS_DIR)/DinnerOSTests

ios-fmt: ## Format Swift sources
	xcrun swift-format format --in-place --recursive --configuration $(IOS_DIR)/.swift-format $(IOS_DIR)/DinnerOS $(IOS_DIR)/DinnerOSTests

# --- Everything ---------------------------------------------------------------

.PHONY: test lint
test: api-test ios-test ## Run all tests
lint: api-lint ios-lint ## Run all linters
