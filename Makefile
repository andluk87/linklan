.PHONY: test integration client server downloads
test:
	CGO_ENABLED=0 go test -mod=vendor -buildvcs=false ./...
	python3 -m unittest discover -s scripts -p 'test_*.py' -v
integration:
	@trap 'docker compose -f compose.test.yml down -v' EXIT; \
	 docker compose -f compose.test.yml up --abort-on-container-exit --exit-code-from tests
client:
	docker build -f Dockerfile.client --target artifacts --output type=local,dest=./releases .
server:
	docker compose up -d --build
downloads:
	python3 scripts/publish-downloads.py
