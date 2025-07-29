
BASE_IMAGE          := scratch

DOCKER				?= docker

GOLANG_BUILD_IMAGE  ?= docker.io/library/golang:1.24.5-bullseye
GOLANG_LINT_IMAGE   := docker.io/golangci/golangci-lint:v2.2.2

ENVIRONMENT         ?= CI
SERVICE				?= dumper

#
# golang
#
# goals fmt, lint, test, build & publish (prefixed with 'go-')
#
.PHONY: go-generate
go-generate: check-SERVICE ## Runs `go generate` within a docker container
	@echo "+++ $$(date) - Running 'go generate'"

ifeq ($(filter $(ENVIRONMENT),local docker),$(ENVIRONMENT))
	cd ${SERVICE} && go generate ./...
else
	${DOCKER} run --rm \
	-v $(PWD):/usr/src/app \
	-w /usr/src/app \
	--entrypoint "/bin/bash" \
	$(GOLANG_BUILD_IMAGE) \
	-c "cd /usr/src/app && go generate ./..."
endif

	@echo "$$(date) - Completed 'go generate'"


.PHONY: go-mod
go-mod: check-SERVICE ## Runs `go mod` within a docker container
	@echo "+++ $$(date) - Running 'go fmt'"

ifeq ($(filter $(ENVIRONMENT),local docker),$(ENVIRONMENT))
	cd ${SERVICE} && go mod tidy
else
	${DOCKER} run --rm \
	-v $(PWD):/usr/src/app \
	-w /usr/src/app \
	--entrypoint "/bin/bash" \
	$(GOLANG_BUILD_IMAGE) \
	-c "cd /usr/src/app && go mod tidy"

endif

.PHONY: go-fmt
go-fmt: check-SERVICE ## Runs `go fmt` within a docker container
	@echo "+++ $$(date) - Running 'go fmt'"

ifeq ($(filter $(ENVIRONMENT),local docker),$(ENVIRONMENT))
	cd ${SERVICE} && go fmt ./...
else
	${DOCKER} run --rm \
	-v $(PWD):/usr/src/app \
	-w /usr/src/app \
	--entrypoint "/bin/bash" \
	$(GOLANG_BUILD_IMAGE) \
	-c "cd /usr/src/app && go fmt ./..."

endif

	@echo "$$(date) - Completed 'go fmt'"

.PHONY: go-lint
go-lint: check-SERVICE ## Runs `golangci-lint run` with more than 60 different linters using golangci-lint within a docker container.
	@echo "+++ $$(date) - Running 'golangci-lint run -v'"

ifeq ($(filter $(ENVIRONMENT),local docker),$(ENVIRONMENT))
	cd ${SERVICE} && golangci-lint run -v
else
	${DOCKER} run --rm \
	-e GOPACKAGESPRINTGOLISTERRORS=1 \
	-e GO111MODULE=on \
	-e GOGC=100 \
	-v $(PWD):/usr/src/app \
	-w /usr/src/app \
	--entrypoint "/bin/bash" \
	$(GOLANG_LINT_IMAGE) \
	-c "golangci-lint run -v"

endif

	@echo "$$(date) - Completed 'golangci-lint run'"

.PHONY: go-test
go-test: check-SERVICE ## Runs `go test` within a docker container
	@echo "+++ $$(date) - Running 'go test'"

ifeq ($(filter $(ENVIRONMENT),local docker),$(ENVIRONMENT))
	cd ${SERVICE} && go test -failfast -cover -coverprofile=coverage.txt -v -p 8 -count=1 ./...
else
	${DOCKER} run --rm \
	-v $(PWD):/usr/src/app \
	-w /usr/src/app \
	--entrypoint=bash \
	$(GOLANG_BUILD_IMAGE) \
	-c "go test -failfast -cover -coverprofile=coverage.txt -v -p 8 -count=1 ./..."

endif

	@echo "+++ $$(date) - Completed 'go test'"

.PHONY: go-build
go-build: check-SERVICE ## Runs `go build` within a docker container
	@echo "+++ $$(date) - Running 'go build' for all go apps"

ifeq ($(filter $(ENVIRONMENT),local docker),$(ENVIRONMENT))
	cd ${SERVICE} && CGO_ENABLED=1 go build -v -o ${SERVICE} -ldflags '-s -w -X main.version=${VERSION_HASH}' cmd/${SERVICE}/main.go
else
	${DOCKER} run --rm \
	-v $(PWD):/usr/src/app \
	-w /usr/src/app \
	--entrypoint=bash \
	$(GOLANG_BUILD_IMAGE) \
	-c "CGO_ENABLED=1 go build -v -o ${SERVICE} -ldflags '-s -w -X main.version=$(VERSION_HASH)' cmd/${SERVICE}/main.go"

endif

	@echo "$$(date) - Completed 'go build'"

.PHONY: go-docker-build
go-docker-build: check-SERVICE ## Runs the build in a multi-stage docker img, requires APP var to be set
	@echo "+++ $$(date) - Running 'go build' for ${SERVICE}"

	docker build \
	--tag=$(DOCKER_REPO):$(SHA1) \
	--tag=$(DOCKER_REPO):latest \
	--build-arg BUILD_IMAGE=$(GOLANG_BUILD_IMAGE) \
	--build-arg BASE_IMAGE=$(BASE_IMAGE) \
	--build-arg VERSION=$(VERSION_HASH) \
	--build-arg SERVICE=${SERVICE} \
	--file cmd/${SERVICE}/Dockerfile .

	@echo "$$(date) - Completed 'go build' for ${SERVICE}"

#
# :kludge: Need to clean-up this section
#

.PHONY: go-run-dev
go-run-dev: check-SERVICE ## Runs the app local within a docker container, requires APP var to be set (excludes go-generate go-sql-generate)
ifeq ($(filter $(ENVIRONMENT),local docker),$(ENVIRONMENT))
	cd ${SERVICE} && go run -x -ldflags "-X main.version=$(VERSION_HASH)" cmd/${SERVICE}/main.go
else
	@echo "Starting backend app"
	${DOCKER} run -it --rm \
	-p 8443:8443 \
	-v $(PWD)/${SERVICE}:/usr/src/app \
	-w /usr/src/app \
	$(GOLANG_BUILD_IMAGE) \
	go run -v -ldflags "-s -w -X main.version=$(VERSION_HASH)" cmd/${SERVICE}/main.go
endif


.PHONY: go-docker-bash
go-docker-bash: check-SERVICE  ## Returns an interactive shell in the golang docker image - useful for debugging
	${DOCKER} run -it --rm \
	--memory=4g \
	-v $(PWD)/${SERVICE}:/usr/src/app \
	-w /usr/src/app \
	--entrypoint "/bin/bash" \
	$(GOLANG_BUILD_IMAGE)

#
#  /end golang
#
