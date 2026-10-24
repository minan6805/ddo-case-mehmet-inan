.PHONY: up down logs smoke verify test test-integration verify-auth verify-tickets verify-sla prove-sla verify-assignment prove-assignment verify-comments verify-email verify-t3 prove-t3 simulate-email prove-email load reset-demo verify-assets prove-assets

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
