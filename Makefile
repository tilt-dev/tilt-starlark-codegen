.PHONY: install check test golden

install:
	go install ./

check:
	golangci-lint run -v --timeout 120s

test:
	go test ./test

golden:
	WRITE_GOLDEN_MASTER=1 go test ./test
