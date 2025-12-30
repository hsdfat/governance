build:
	go build -o bin/main cmd/telco_manager/main.go
run: build
	./bin/main
