install:
	go install ./

check:
	golangci-lint run -v --timeout 120s
