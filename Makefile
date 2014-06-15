all: deps
	cd server;go build -o $(GOPATH)/bin/server
	cd local;go build -o $(GOPATH)/bin/local

deps:
	./deps.sh

clean:
	rm -f $(GOPATH)/bin/server
	rm -f $(GOPATH)/bin/local
