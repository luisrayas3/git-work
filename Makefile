UNAME_S := $(shell uname -s)
XARGS:=xargs -r
ifeq ($(UNAME_S),Darwin)
    XARGS:=xargs
endif

TAG:=$(shell git describe --match 'v*' --always --dirty --broken)
LDFLAGS:=-X main.version="${TAG}"

all: build

.PHONY: build
build:
	go generate
	go build -ldflags "$(LDFLAGS)" .

# produce a debugger-friendly build
.PHONY: build/debug
build/debug:
	go generate
	go build -ldflags "$(LDFLAGS)" -gcflags=all="-N -l" .

.PHONY: install
install:
	go generate
	go install -ldflags "$(LDFLAGS)" .

.PHONY: secure
secure:
	go tool govulncheck ./...

.PHONY: test
test:
	go test -v -bench=. ./...

.PHONY: clean-local-issues
clean-local-issues:
	git for-each-ref refs/issues/ | cut -f 2 | $(XARGS) -n 1 git update-ref -d
	git for-each-ref refs/remotes/origin/issues/ | cut -f 2 | $(XARGS) -n 1 git update-ref -d
	rm -f .git/git-work/cache/issues

.PHONY: clean-remote-issues
clean-remote-issues:
	git ls-remote origin "refs/issues/*" | cut -f 2 | $(XARGS) git push origin -d

.PHONY: clean-local-identities
clean-local-identities:
	git for-each-ref refs/work-users/ | cut -f 2 | $(XARGS) -n 1 git update-ref -d
	git for-each-ref refs/remotes/origin/work-users/ | cut -f 2 | $(XARGS) -n 1 git update-ref -d
	rm -f .git/git-work/cache/work-users

.PHONY: clean-remote-identities
clean-remote-identities:
	git ls-remote origin "refs/work-users/*" | cut -f 2 | $(XARGS) git push origin -d
