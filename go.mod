module github.com/Silo-Server/silo-plugin-tmdb

go 1.26.0

require (
	golang.org/x/time v0.14.0
	google.golang.org/protobuf v1.36.11
)

require google.golang.org/grpc v1.82.1 // indirect

require (
	github.com/Silo-Server/silo-plugin-sdk v0.13.2
	github.com/fatih/color v1.13.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.7.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.17 // indirect
	github.com/oklog/run v1.1.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
)

replace github.com/Silo-Server/silo-plugin-sdk => github.com/blurbery/silo-plugin-sdk v0.0.0-20260821084713-0029dbbc36be
