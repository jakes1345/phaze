# Phaze™ build system

.PHONY: all android android-aab nexus clean help test vet verify web-build build-all phaze-assets desktop

APP_NAME=Phaze
PACKAGE_ID=world.phazechat.app
# Single source of truth: repo-root VERSION file stamps the Go server build via
# -ldflags. Android reads its version from android/app/build.gradle.kts
# (versionCode must increment by hand for Play) and shows it via BuildConfig.
VERSION=$(shell cat VERSION 2>/dev/null || echo dev)
GO_LDFLAGS=-X main.Version=$(VERSION)
# Android SDK (default). Override with env or copy local.mk.example → local.mk
ANDROID_HOME ?= $(HOME)/Android/Sdk
-include local.mk

all: help

build-all: ## Full stack: tests → nexus server → web → Android AAB
	@echo "═══════════════════════════════════════════════════════════════════"
	@echo " Phaze build-all: test → nexus → web-build → android-aab"
	@echo "═══════════════════════════════════════════════════════════════════"
	@$(MAKE) test nexus web-build android-aab
	@echo ""
	@echo "[Phaze] build-all done. Artifacts:"
	@ls -la bin/ 2>/dev/null || true
	@echo "  web/dist/  (static web client)"

web-build: ## Production build of web/ (requires npm)
	@echo "[Phaze] Building web client..."
	cd web && npm ci && npm run build

desktop: ## Build Phaze desktop app via Wails → desktop/build/bin/Phaze
	@echo "[Phaze] Building desktop client..."
	cd web && npm ci && VITE_BASE=/ npm run build
	rm -rf desktop/dist && cp -r web/dist desktop/dist
	cd desktop && ~/go/bin/wails build -tags webkit2_41
	@echo "[Phaze] Desktop binary → desktop/build/bin/Phaze"

## 📱 Android (Kotlin/Compose — the Google Play client)
android: ## Build a debug APK → android/app/build/outputs/apk/debug/
	@echo "[Phaze] Building Android debug APK..."
	cd android && ./gradlew :app:assembleDebug --no-daemon

android-aab: ## Build signed release AAB for Google Play → bin/Phaze-release.aab
	@echo "[Phaze] Building Android App Bundle (AAB) for Play Store..."
	@if [ ! -f android/local.properties ]; then \
		echo "ERROR: android/local.properties missing (needs sdk.dir + signing keys)."; \
		exit 1; fi
	@mkdir -p bin
	cd android && ./gradlew bundleRelease --no-daemon
	cp android/app/build/outputs/bundle/release/app-release.aab bin/Phaze-release.aab
	@echo "[Phaze] AAB → bin/Phaze-release.aab  (upload this to Google Play Console)"

## 🌐 Server
nexus: ## Build the Nexus Relay Server → bin/phaze-nexus
	@echo "[Phaze] Building Nexus Relay Server..."
	@mkdir -p bin
	cd nexus_server && go build -ldflags="$(GO_LDFLAGS)" -o ../bin/phaze-nexus .

## 🎨 Assets
phaze-assets: ## Regenerate WAV sounds + PNG emoticons → nexus_server/public/phaze/assets/
	@echo "[Phaze] Generating sounds → nexus_server/public/phaze/assets/sounds ..."
	@mkdir -p nexus_server/public/phaze/assets/sounds nexus_server/public/phaze/assets/emoticons
	cd nexus_server && go run ./cmd/soundgen "public/phaze/assets/sounds"
	@echo "[Phaze] Generating emoticons + branding → nexus_server/public/phaze/assets ..."
	cd nexus_server && go run ./cmd/emoticongen "public/phaze/assets"
	@echo "[Phaze] Refreshing Nexus default avatar (ServeFile path) ..."
	@mkdir -p nexus_server/assets
	cp -f nexus_server/public/phaze/assets/default_avatar.png nexus_server/assets/default_avatar.png 2>/dev/null || true

## 🛡️ Maintenance
test: ## Run Go server tests
	@echo "[Phaze] Running tests..."
	cd nexus_server && go test ./...

vet: ## go vet on nexus_server
	@echo "[Phaze] go vet..."
	cd nexus_server && go vet ./...

verify: test vet ## tests + vet + web build + ESLint (needs npm)
	@echo "[Phaze] Web verify..."
	cd web && npm ci && npm run build && npm run test && npm run lint

clean: ## Purge all build artifacts
	@echo "[Phaze] Purging artifacts..."
	rm -rf bin/

help: ## Show this help menu
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
