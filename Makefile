.PHONY: help run-local build-local docker-build docker-up podman-build podman-up android-sync android-open android-build android-clean

# Ensure Gradle runs with JDK 17 for compatibility if available on the system
export JAVA_HOME ?= $(shell if [ -d "/usr/lib/jvm/java-1.17.0-openjdk-amd64" ]; then echo "/usr/lib/jvm/java-1.17.0-openjdk-amd64"; else echo "$$JAVA_HOME"; fi)

# Default target: show help
help:
	@echo "Available build targets:"
	@echo "  Go Server (Local):"
	@echo "    make run-local     - Run the Go server locally"
	@echo "    make build-local   - Compile the Go server binary locally"
	@echo ""
	@echo "  Docker Containers:"
	@echo "    make docker-build  - Build the Docker image using Docker Compose"
	@echo "    make docker-up     - Run the server in a Docker container"
	@echo ""
	@echo "  Podman Containers:"
	@echo "    make podman-build  - Build the Podman image using podman-compose"
	@echo "    make podman-up     - Run the server in a Podman container"
	@echo ""
	@echo "  Android App (Capacitor):"
	@echo "    make android-sync  - Sync web assets in 'www' to the Android platform"
	@echo "    make android-open  - Open the Android project in Android Studio"
	@echo "    make android-build - Build the debug APK using Gradle"
	@echo "    make android-clean - Clean Gradle build artifacts in the Android project"

# --- Go Server (Local) ---
run-local:
	go run main.go

build-local:
	go build -o pinochle-server .

# --- Docker ---
docker-build:
	docker compose build

docker-up:
	docker compose up

# --- Podman ---
podman-build:
	podman-compose build

podman-up:
	podman-compose up

# --- Android (Capacitor) ---
android-sync:
	npx cap sync

android-open:
	npx cap open android

android-build:
	cd android && ./gradlew assembleDebug

android-clean:
	cd android && ./gradlew clean
