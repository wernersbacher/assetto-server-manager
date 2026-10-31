VERSION?=unstable
ifeq ($(VERSION),)
VERSION := "unstable"
endif

# enable go modules
export GO111MODULE=on

all: clean vet test assets build

install-linter:
	which golangci-lint || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.27.0

clean:
	rm -rf internal/changelog/changelog_embed.go
	$(MAKE) -C cmd/acpitwall clean

test:
	mkdir -p cmd/acpitwall/assetto/cfg
	mkdir -p cmd/acpitwall/assetto/results
	cp -R fixtures/results/*.json cmd/acpitwall/assetto/results
	go test -race

vet: install-linter generate
	go vet ./...
	golangci-lint -E bodyclose,misspell,gofmt,golint,unconvert,goimports,depguard,interfacer run --timeout 30m --skip-files car_skins.go,kissmyrank_config.go,realpenalty_config.go

generate:
	go get -u github.com/mjibson/esc
	go generate ./...

assets:
	$(MAKE) -C cmd/acpitwall assets

asset-embed: generate
	$(MAKE) -C cmd/acpitwall asset-embed

build:
	$(MAKE) -C cmd/acpitwall build

deploy: clean generate vet test
	$(MAKE) -C cmd/acpitwall deploy

run:
	$(MAKE) -C cmd/acpitwall run

docker:
	docker build --build-arg SM_VERSION=${VERSION} -t ghcr.io/wernersbacher/acpitwall:${VERSION} .
	docker push ghcr.io/wernersbacher/acpitwall:${VERSION}
ifneq ("${VERSION}", "unstable")
	docker tag ghcr.io/wernersbacher/acpitwall:${VERSION} ghcr.io/wernersbacher/acpitwall:latest
	docker push ghcr.io/wernersbacher/acpitwall:latest
endif
