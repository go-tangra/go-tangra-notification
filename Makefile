# Makefile for Notification Service

include ../../../app.mk

NOTIFICATION_IMAGE_NAME ?= menta2l/notification-service
NOTIFICATION_IMAGE_TAG ?= $(VERSION)
DOCKER_REGISTRY ?=

.PHONY: build-server
build-server:
	@echo "Building Notification server..."
	@go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o ./bin/notification-server ./cmd/server

.PHONY: docker
docker:
	@echo "Building Docker image $(NOTIFICATION_IMAGE_NAME):$(NOTIFICATION_IMAGE_TAG)..."
	@docker build \
		-t $(NOTIFICATION_IMAGE_NAME):$(NOTIFICATION_IMAGE_TAG) \
		-t $(NOTIFICATION_IMAGE_NAME):latest \
		--build-arg APP_VERSION=$(VERSION) \
		-f ./Dockerfile \
		.

.PHONY: docker-tag
docker-tag: docker
ifdef DOCKER_REGISTRY
	@echo "Tagging image for registry $(DOCKER_REGISTRY)..."
	@docker tag $(NOTIFICATION_IMAGE_NAME):$(NOTIFICATION_IMAGE_TAG) $(DOCKER_REGISTRY)/$(NOTIFICATION_IMAGE_NAME):$(NOTIFICATION_IMAGE_TAG)
	@docker tag $(NOTIFICATION_IMAGE_NAME):latest $(DOCKER_REGISTRY)/$(NOTIFICATION_IMAGE_NAME):latest
endif

.PHONY: docker-push
docker-push: docker-tag
ifdef DOCKER_REGISTRY
	@echo "Pushing image to $(DOCKER_REGISTRY)..."
	@docker push $(DOCKER_REGISTRY)/$(NOTIFICATION_IMAGE_NAME):$(NOTIFICATION_IMAGE_TAG)
	@docker push $(DOCKER_REGISTRY)/$(NOTIFICATION_IMAGE_NAME):latest
else
	@echo "Pushing image to Docker Hub..."
	@docker push $(NOTIFICATION_IMAGE_NAME):$(NOTIFICATION_IMAGE_TAG)
	@docker push $(NOTIFICATION_IMAGE_NAME):latest
endif

.PHONY: run-server
run-server:
	@go run ./cmd/server -c ./configs

.PHONY: ent
ent:
ifneq ("$(wildcard ./internal/data/ent)","")
	@ent generate \
		--feature sql/modifier \
		--feature sql/upsert \
		--feature sql/lock \
		./internal/data/ent/schema
endif

.PHONY: wire
wire:
	@cd ./cmd/server && wire

.PHONY: test
test:
	@go test -v ./...

.PHONY: test-cover
test-cover:
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: clean
clean:
	@rm -rf ./bin
	@rm -f coverage.out coverage.html
	@echo "Clean complete!"

.PHONY: generate
generate: ent wire
	@echo "Generation complete!"
