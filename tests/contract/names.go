package contract

import "google.golang.org/protobuf/reflect/protoreflect"

func protoName(s string) protoreflect.Name { return protoreflect.Name(s) }
