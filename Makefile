all:
	cd server;go build -o $(GOPATH)/bin/server;
	cd local;go build -o $(GOPATH)/bin/local;
