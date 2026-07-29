build:
	go build -o bin/yadb .

run:
	go run .

test:
	go test ./...

install:
	go install .
