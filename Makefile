# Following gets combined into: REGISTRY/REGISTRY_PATH/IMAGE:VERSION
REGISTRY?=csemcr.azurecr.io
REGISTRY_PATH?=test/k8s/metrics
IMAGE?=adapter
VERSION?=latest

OUT_DIR?=./_output
SEMVER=""
PUSH_LATEST=true

ifeq ("$(REGISTRY_PATH)", "")
	FULL_IMAGE=$(REGISTRY)/$(IMAGE)
else
	FULL_IMAGE=$(REGISTRY)/$(REGISTRY_PATH)/$(IMAGE)
endif	

BRANCH=$(shell git rev-parse --abbrev-ref HEAD)

.PHONY: all build-local build vendor test version push \
		verify-deploy gen-deploy dev save tag-ci

all: build
build-local: test
	CGO_ENABLED=0 go build -a -tags netgo -o $(OUT_DIR)/adapter github.com/Azure/azure-k8s-metrics-adapter

build: vendor verify-deploy
	docker build -t $(FULL_IMAGE):$(VERSION) .

vendor: 
	dep ensure 

test: vendor
	CGO_ENABLED=0 go test ./pkg/...

version: build	
ifeq ("$(SEMVER)", "")
	@echo "Please set sem version bump: can be 'major', 'minor', or 'patch'"
	exit
endif
ifeq ("$(BRANCH)", "master")
	@echo "versioning on master"
	go get -u github.com/jsturtevant/gitsem
	gitsem $(SEMVER)
else
	@echo "must be on clean master branch"
endif	

push:
	@echo $(DOCKER_PASS) | docker login -u $(DOCKER_USER) --password-stdin csemcr.azurecr.io 
	docker push $(FULL_IMAGE):$(VERSION)
ifeq ("$(PUSH_LATEST)", "true")
	@echo "pushing to latest"
	docker tag $(FULL_IMAGE):$(VERSION) $(FULL_IMAGE):latest
	docker push $(FULL_IMAGE):latest
endif		

# Code generation commands
verify-deploy:
	hack/verify-deploy.sh

gen-deploy:
	hack/gen-deploy.sh

# dev setup
dev:
	skaffold dev

# CI specific commands used during CI build
save:
	docker save -o app.tar $(FULL_IMAGE):$(VERSION)

tag-ci:
	docker tag $(FULL_IMAGE):$(CIRCLE_WORKFLOW_ID) $(FULL_IMAGE):$(VERSION)
	

