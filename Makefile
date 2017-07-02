all:
	cd server;go get -v -d; go build -o $(GOPATH)/bin/server
	cd local;go get -v -d; go build -o $(GOPATH)/bin/local

clean:
	rm -f $(GOPATH)/bin/server
	rm -f $(GOPATH)/bin/local
