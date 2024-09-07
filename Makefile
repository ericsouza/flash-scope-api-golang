APP_NAME=barry-allen
APP_VERSION="$(shell mvn help:evaluate -Dexpression=project.version -q -DforceStdout)"
REPO=$(APP_NAME)
DOCKER_COMPOSE_FILE_PATH?="docker-compose.yaml"
SEMVER_INCREMENT?="patch"
DOCKER_APP_VERSION?=dev

BENCHMARK_DURATION?=20s
DEV_JWT_TOKEN?=Bearer eyJ0eXAiOiJKV1QiLCJhbGciOiJub25lIn0.eyJzdWIiOiJERjU4MEY1MTRGQ0Y2QzZFMjM4ODE0OTc3MENBOUJCQiJ9.
APP_MIN_RPS_REQUIRED=1500
#below MAX_LATENCY is nanoseconds
APP_MAX_95_PERCENTILE_LATENCY_REQUIRED=25000000
BENCHMARK_RATE=1650

.PHONY: all restart package test unit-test start build-prod build-dev stop clean status logs setup prod bump-version git-release start-side-containers benchmark-warmup benchmark-run benchmark-validate benchmark-test benchmark-create benchmark-pop

all: clean build-dev start

restart: stop start

package:
	@mvn clean package -DskipTests

package-native:
	@mvn clean package -Pnative -DskipTests

test: unit-test

unit-test:
	@mvn test

start:
	@echo "Starting containers..."
	@echo "$(APP_NAME) version: $(APP_VERSION)"
	@APP_VERSION=$(DOCKER_APP_VERSION) docker compose -f ${DOCKER_COMPOSE_FILE_PATH} up -d

build-prod: package
	@docker build --output type=docker -t $(REPO):$(APP_VERSION) -f src/docker/Dockerfile.jvm .

build-dev: package
	@docker build --output type=docker -t $(REPO):$(DOCKER_APP_VERSION) -f src/docker/Dockerfile.jvm .

stop:
	@echo "Stopping containers..."
	@docker compose -f ${DOCKER_COMPOSE_FILE_PATH} stop

clean: stop
	@docker compose -f ${DOCKER_COMPOSE_FILE_PATH} rm -f

status:
	@docker compose -f ${DOCKER_COMPOSE_FILE_PATH} ps

logs:
	@docker compose -f ${DOCKER_COMPOSE_FILE_PATH} logs -f $(APP_NAME)

setup:
	@echo "Creating network 'development'..."
	@docker network create --gateway 172.28.0.1 --subnet 172.28.0.0/16 development 2>/dev/null; true
	@echo "Generating 'application.properties'"
	@cp -n src/main/resources/application.properties.sample src/main/resources/application.properties; true

prod: bump-version build-prod git-release
	@echo "\n\nTarget 'prod' for now is doing nothing, but usually would do a 'docker push ${REPO}:$(APP_VERSION)'"

bump-version:
	@mvn semver:increment-$(SEMVER_INCREMENT) -DprocessModule
	@echo "the following version will be released"
	@mvn help:evaluate -Dexpression=project.version -q -DforceStdout && echo ""

git-release:
	@git add pom.xml \
		&& git commit -m "release version $(APP_VERSION)" \
		&& git push --set-upstream origin main

start-side-containers:
	@echo "Starting containers except for $(APP_NAME) service"
	@APP_VERSION=$(DOCKER_APP_VERSION) docker compose -f ${DOCKER_COMPOSE_FILE_PATH} up -d --scale ${APP_NAME}=0

benchmark-warmup:
	@echo -n "Warming up. Please wait..."
	@vegeta attack -targets=benchmarks/vegeta-add-and-pop.txt -duration=15s -rate=$(BENCHMARK_RATE) -max-workers=50 1>/dev/null
	@echo "Done!"

benchmark-run: benchmark-warmup
	@echo -n "Running benchmark. Please wait..."
	@vegeta attack -targets=benchmarks/vegeta-add-and-pop.txt -duration=30s -rate=$(BENCHMARK_RATE) -max-workers=50 | \
	tee vegeta-results.bin | vegeta report -type json > tmp-benchmark-result.json && rm vegeta-results.bin
	@echo "Done!"

benchmark-validate:
	@echo "\nValidating benchmark results...\n"
	$(eval RESULT_RPS := $(shell cat tmp-benchmark-result.json | jq '."rate"' | cut -d . -f 1))
	$(eval RESULT_P95_LATENCY := $(shell cat tmp-benchmark-result.json | jq '."latencies"."95th"'))

	@rm -f tmp-benchmark-result.json
	@touch tmp-benchmarks-validate-result.txt
	@if [ $(RESULT_RPS) -lt $(APP_MIN_RPS_REQUIRED) ]; then echo "Flashes RPS Benchmark failed.\nExpected at least: $(APP_MIN_RPS_REQUIRED)\nActual Result: $(RESULT_RPS)\n" && echo "FAILED_BENCHMARKS_TESTS=TRUE" > tmp-benchmarks-validate-result.txt; else echo "Flashes RPS Benchmark passed!\n"; fi
	@if [ $(RESULT_P95_LATENCY) -gt $(APP_MAX_95_PERCENTILE_LATENCY_REQUIRED) ]; then echo "Flashes P95 Latency Benchmark failed.\nExpected a maximum of: $(APP_MAX_95_PERCENTILE_LATENCY_REQUIRED)\nActual Result: $(RESULT_P95_LATENCY)\n" && echo "FAILED_BENCHMARKS_TESTS=TRUE" > tmp-benchmarks-validate-result.txt; else echo "Flashes P95 Latency Benchmark passed!\n"; fi

	@if [ $$(cat tmp-benchmarks-validate-result.txt | wc -l) -gt 0 ]; then echo "There are benchmarks tests failing, exiting now..." && rm -f tmp-benchmarks-validate-result.txt && exit 1; else echo "All benchmark tests passed!! Congrats!" && rm -f tmp-benchmarks-validate-result.txt; fi

benchmark-test: benchmark-run benchmark-validate

benchmark-create:
	@vegeta attack -targets=benchmarks/vegeta-add.txt -duration=20s -rate=600 -rate=$(BENCHMARK_RATE) -max-workers=50 | \
	tee vegeta-results.bin | vegeta report -type json | jq . && rm vegeta-results.bin

benchmark-pop:
	@vegeta attack -targets=benchmarks/vegeta-pop.txt -duration=20s -rate=600 -rate=$(BENCHMARK_RATE) -max-workers=50 | \
	tee vegeta-results.bin | vegeta report -type json | jq . && rm vegeta-results.bin
