build:
	docker build -t ghcr.io/spy4x/oko:dev -f Dockerfile .

push: build
	docker push ghcr.io/spy4x/oko:latest
	docker push ghcr.io/spy4x/oko:$$(git rev-parse --short HEAD)

run: build
	docker run --rm -p 8080:8080 \
	  -e DOMAIN=antonshubin.com \
	  -e UPTIME_HOSTS=uptime-cloud.antonshubin.com,uptime-home.antonshubin.com \
	  ghcr.io/spy4x/oko:dev

test:
	go test ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...
