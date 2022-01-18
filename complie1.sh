rm -f testgo
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o testgo -ldflags="-s -w" . 
