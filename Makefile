# Images are published by CI (.woodpecker.yml); these targets are for
# local work only.

build:
	docker build -t ghcr.io/spy4x/oko:dev -f Dockerfile .

run: build
	docker run --rm -p 8080:8080 \
	  -e DOMAIN=example.com \
	  -e UPTIME_HOSTS=uptime-cloud.example.com,uptime-home.example.com \
	  ghcr.io/spy4x/oko:dev

test:
	go test ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

# The demo video in media/demo/: see media/demo/recipe/README.md.
demo-video:
	cd media/demo/recipe && deno task record

demo-video-encode:
	cd media/demo/recipe && deno task encode

.PHONY: build run test fmt vet demo-video demo-video-encode
