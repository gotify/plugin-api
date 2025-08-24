package plugin

//go:generate protoc -Iprotobuf --go_out=./generated/protobuf --go_opt=paths=source_relative --go-grpc_out=./generated/protobuf --go-grpc_opt=paths=source_relative ./protobuf/meta.proto ./protobuf/config.proto ./protobuf/display.proto
