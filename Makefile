IMAGE ?= browser-stream:mvp
CONTAINER ?= browser-stream
HOST_PORT ?= 8080
BROWSER_URL ?= https://google.com
VIDEO_WIDTH ?= 1920
VIDEO_HEIGHT ?= 1080
VIDEO_FPS ?= 60
VIDEO_BITRATE ?= 6000k
VIDEO_PROFILE ?= 720p60
AUDIO_BITRATE ?= 32k
BRAVE_PROFILE_VOLUME ?= browser-stream-brave-profile
WIDEVINE_ARCH ?= $(shell uname -m)
WIDEVINE_PLATFORM := $(if $(filter aarch64 arm64,$(WIDEVINE_ARCH)),linux_arm64,$(if $(filter x86_64 amd64,$(WIDEVINE_ARCH)),linux_x64))
WIDEVINE_SEARCH_DIRS ?= $(CURDIR)/widevine /opt/WidevineCdm /opt/brave.com/brave/WidevineCdm
widevine_bundle_dir = $(if $(and $(WIDEVINE_PLATFORM),$(wildcard $(1)/manifest.json),$(wildcard $(1)/_platform_specific/$(WIDEVINE_PLATFORM)/libwidevinecdm.so)),$(1))
AUTO_WIDEVINE_DIR := $(firstword $(foreach directory,$(WIDEVINE_SEARCH_DIRS),$(call widevine_bundle_dir,$(directory))))
WIDEVINE_DIR ?= $(AUTO_WIDEVINE_DIR)
TAILSCALE_IP := $(shell tailscale ip -4 2>/dev/null | head -n 1)
WEBRTC_ICE_HOST ?= $(if $(TAILSCALE_IP),$(TAILSCALE_IP),127.0.0.1)
WEBRTC_UDP_PORT_MIN ?= 50000
WEBRTC_UDP_PORT_MAX ?= 50010

.PHONY: help test build run logs stop smoke widevine-status clean rerun

help: ## List available commands
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "%-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run all automated tests
	go test ./...
	node --test web/*.test.mjs
	bash scripts/brave_test.sh
	bash scripts/widevine_test.sh
	bash scripts/make_run_test.sh

build: ## Build the Docker image
	docker build -t $(IMAGE) .

run: ## Start the local browser-stream container
	docker run --rm --detach --name $(CONTAINER) \
		--hostname browser-stream \
		--publish $(HOST_PORT):8080 \
		--publish $(WEBRTC_UDP_PORT_MIN)-$(WEBRTC_UDP_PORT_MAX):$(WEBRTC_UDP_PORT_MIN)-$(WEBRTC_UDP_PORT_MAX)/udp \
		--volume "$(BRAVE_PROFILE_VOLUME):/var/lib/browser-stream/brave-profile" \
		$(if $(strip $(WIDEVINE_DIR)),--volume "$(abspath $(WIDEVINE_DIR)):/opt/brave.com/brave/WidevineCdm:ro") \
		--env BROWSER_URL="$(BROWSER_URL)" \
		$(if $(filter undefined,$(origin BROWSER_USER_AGENT)),,--env BROWSER_USER_AGENT="$(BROWSER_USER_AGENT)") \
		--env VIDEO_WIDTH="$(VIDEO_WIDTH)" \
		--env VIDEO_HEIGHT="$(VIDEO_HEIGHT)" \
		--env VIDEO_FPS="$(VIDEO_FPS)" \
		--env VIDEO_BITRATE="$(VIDEO_BITRATE)" \
		--env VIDEO_PROFILE="$(VIDEO_PROFILE)" \
		--env AUDIO_BITRATE="$(AUDIO_BITRATE)" \
		--env WEBRTC_ICE_HOST="$(WEBRTC_ICE_HOST)" \
		--env WEBRTC_UDP_PORT_MIN="$(WEBRTC_UDP_PORT_MIN)" \
		--env WEBRTC_UDP_PORT_MAX="$(WEBRTC_UDP_PORT_MAX)" \
		$(IMAGE)

logs: ## Follow container logs
	docker logs --follow $(CONTAINER)

stop: ## Stop the local container
	docker stop $(CONTAINER)

smoke: ## Check the local health endpoint
	@for attempt in $$(seq 1 20); do \
		if curl --fail --silent http://localhost:$(HOST_PORT)/healthz; then echo; exit 0; fi; \
		sleep 1; \
	done; \
	exit 1

widevine-status: ## Show the detected Widevine bundle
	@if [ -z "$(WIDEVINE_PLATFORM)" ]; then \
		echo "Unsupported architecture: $(WIDEVINE_ARCH)"; \
	elif [ -n "$(WIDEVINE_DIR)" ]; then \
		echo "Widevine $(WIDEVINE_PLATFORM): $(abspath $(WIDEVINE_DIR))"; \
	else \
		echo "Widevine $(WIDEVINE_PLATFORM): not found"; \
		echo "Searched: $(WIDEVINE_SEARCH_DIRS)"; \
	fi

clean: ## Stop the local container and remove the Docker image
	-docker stop $(CONTAINER)
	docker image rm $(IMAGE)

rerun: stop build run
