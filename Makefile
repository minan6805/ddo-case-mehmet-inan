.PHONY: verify-openapi lint simulate verify-simulator prove-simulator verify-reports prove-reports verify-knowledge prove-knowledge up down logs smoke verify test test-integration verify-auth verify-tickets verify-sla prove-sla verify-assignment prove-assignment verify-comments verify-email verify-t3 prove-t3 simulate-email prove-email load reset-demo verify-assets prove-assets verify-changes verify-t4 prove-t4

up:
	docker compose up --build -d --wait

down:
	docker compose down

logs:
	docker compose logs --tail=100 -f

smoke:
	python3 scripts/verify.py

verify:
	python3 scripts/verify.py --full

test:
	docker build --target test -t destekmasa-api-test ./backend
	docker run --rm destekmasa-api-test

test-integration:
	sh scripts/test-integration.sh

verify-auth:
	python3 scripts/verify.py --auth

verify-tickets:
	python3 scripts/verify.py --tickets

verify-sla:
	python3 scripts/verify.py --sla

prove-sla:
	sh scripts/test-integration.sh

verify-t3:
	python3 scripts/verify.py --email --comments --threads --t3

prove-t3:
	TEST_PACKAGE=./internal/tickets TEST_RUN='TestPostgresEmailWorkflow|TestPostgresThreadWorkflow' sh scripts/test-integration.sh

verify-email:
	python3 scripts/verify.py --email

simulate-email:
	docker compose --profile tools run --rm email-simulator

prove-email:
	TEST_PACKAGE=./internal/tickets TEST_RUN=TestPostgresEmailWorkflow sh scripts/test-integration.sh

verify-comments:
	python3 scripts/verify.py --comments

verify-assignment:
	python3 scripts/verify.py --assignment

prove-assignment:
	TEST_PACKAGE=./internal/assignment TEST_RUN=TestPostgresAssignmentEngine sh scripts/test-integration.sh

load:
	docker compose --profile load run --rm load

reset-demo:
	@echo 'Bu komut bu projeye ait PostgreSQL verilerini siler. Onay için: make reset-demo CONFIRM=yes'
	@test "$(CONFIRM)" = yes
	docker compose down --volumes
	docker compose up --build -d --wait

verify-assets:
	python3 scripts/verify.py --assets

prove-assets:
	TEST_PACKAGE=./internal/assets TEST_RUN=TestPostgresAssetGraph sh scripts/test-integration.sh

verify-changes:
	python3 scripts/verify.py --changes

verify-t4:
	python3 scripts/verify.py --assets --changes --t4

prove-t4:
	TEST_RUN='TestPostgresAssetGraph|TestPostgresChangeWorkflow|TestReachable' sh scripts/test-integration.sh

verify-knowledge:
	python3 scripts/verify.py --knowledge

prove-knowledge:
	TEST_PACKAGE=./internal/knowledge TEST_RUN=TestPostgresKnowledge sh scripts/test-integration.sh

verify-reports:
	python3 scripts/verify.py --reports

prove-reports:
	TEST_PACKAGE=./internal/reports TEST_RUN=TestPostgresReports sh scripts/test-integration.sh

simulate:
	docker compose --profile tools run --build --rm simulator

verify-simulator:
	python3 scripts/verify.py --simulator

prove-simulator:
	TEST_PACKAGE=./internal/simulator TEST_RUN=TestPostgresSimulator sh scripts/test-integration.sh

# Keep this version aligned with the GitHub Actions lint step.
lint:
	docker run --rm -v "$$(pwd)/backend:/app" -w /app golangci/golangci-lint:v2.1.6 sh -ec 'unformatted_files="$$(gofmt -l .)"; if [ -n "$$unformatted_files" ]; then printf "%s\n" "$$unformatted_files"; exit 1; fi; golangci-lint run'

# Validator dependencies exist only in the disposable tools container.
verify-openapi:
	docker run --rm -v "$$(pwd)/backend/internal/httpapi/openapi.json:/spec/openapi.json:ro" -v "$$(pwd)/scripts/validate-openapi.py:/tools/validate-openapi.py:ro" python:3.13-alpine sh -ec 'pip install --quiet --root-user-action=ignore openapi-spec-validator==0.7.2; python /tools/validate-openapi.py /spec/openapi.json'
