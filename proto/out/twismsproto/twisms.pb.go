// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.32.0
// 	protoc        v4.24.4
// source: twisms.proto

package twismsproto

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// A text message.
type Message struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The phone number of the sender.
	From string `protobuf:"bytes,1,opt,name=from,proto3" json:"from,omitempty"`
	// The phone number of the recipient.
	To string `protobuf:"bytes,2,opt,name=to,proto3" json:"to,omitempty"`
	// The body of the message.
	Body *MessageBody `protobuf:"bytes,3,opt,name=body,proto3" json:"body,omitempty"`
}

func (x *Message) Reset() {
	*x = Message{}
	if protoimpl.UnsafeEnabled {
		mi := &file_twisms_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Message) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Message) ProtoMessage() {}

func (x *Message) ProtoReflect() protoreflect.Message {
	mi := &file_twisms_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Message.ProtoReflect.Descriptor instead.
func (*Message) Descriptor() ([]byte, []int) {
	return file_twisms_proto_rawDescGZIP(), []int{0}
}

func (x *Message) GetFrom() string {
	if x != nil {
		return x.From
	}
	return ""
}

func (x *Message) GetTo() string {
	if x != nil {
		return x.To
	}
	return ""
}

func (x *Message) GetBody() *MessageBody {
	if x != nil {
		return x.Body
	}
	return nil
}

// The body of the message, which may be a text message (SMS) or a richer
// message.
type MessageBody struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to Body:
	//
	//	*MessageBody_Text
	Body isMessageBody_Body `protobuf_oneof:"body"`
}

func (x *MessageBody) Reset() {
	*x = MessageBody{}
	if protoimpl.UnsafeEnabled {
		mi := &file_twisms_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MessageBody) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MessageBody) ProtoMessage() {}

func (x *MessageBody) ProtoReflect() protoreflect.Message {
	mi := &file_twisms_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MessageBody.ProtoReflect.Descriptor instead.
func (*MessageBody) Descriptor() ([]byte, []int) {
	return file_twisms_proto_rawDescGZIP(), []int{1}
}

func (m *MessageBody) GetBody() isMessageBody_Body {
	if m != nil {
		return m.Body
	}
	return nil
}

func (x *MessageBody) GetText() *TextBody {
	if x, ok := x.GetBody().(*MessageBody_Text); ok {
		return x.Text
	}
	return nil
}

type isMessageBody_Body interface {
	isMessageBody_Body()
}

type MessageBody_Text struct {
	Text *TextBody `protobuf:"bytes,1,opt,name=text,proto3,oneof"`
}

func (*MessageBody_Text) isMessageBody_Body() {}

// A plain text message body for an SMS. The content length is limited to 160
// bytes.
type TextBody struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The text of the message.
	Text string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
}

func (x *TextBody) Reset() {
	*x = TextBody{}
	if protoimpl.UnsafeEnabled {
		mi := &file_twisms_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *TextBody) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TextBody) ProtoMessage() {}

func (x *TextBody) ProtoReflect() protoreflect.Message {
	mi := &file_twisms_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TextBody.ProtoReflect.Descriptor instead.
func (*TextBody) Descriptor() ([]byte, []int) {
	return file_twisms_proto_rawDescGZIP(), []int{2}
}

func (x *TextBody) GetText() string {
	if x != nil {
		return x.Text
	}
	return ""
}

// A generic filter for messages.
type MessageFilter struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to Filter:
	//
	//	*MessageFilter_MatchFrom
	//	*MessageFilter_MatchTo
	Filter isMessageFilter_Filter `protobuf_oneof:"filter"`
}

func (x *MessageFilter) Reset() {
	*x = MessageFilter{}
	if protoimpl.UnsafeEnabled {
		mi := &file_twisms_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MessageFilter) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MessageFilter) ProtoMessage() {}

func (x *MessageFilter) ProtoReflect() protoreflect.Message {
	mi := &file_twisms_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MessageFilter.ProtoReflect.Descriptor instead.
func (*MessageFilter) Descriptor() ([]byte, []int) {
	return file_twisms_proto_rawDescGZIP(), []int{3}
}

func (m *MessageFilter) GetFilter() isMessageFilter_Filter {
	if m != nil {
		return m.Filter
	}
	return nil
}

func (x *MessageFilter) GetMatchFrom() string {
	if x, ok := x.GetFilter().(*MessageFilter_MatchFrom); ok {
		return x.MatchFrom
	}
	return ""
}

func (x *MessageFilter) GetMatchTo() string {
	if x, ok := x.GetFilter().(*MessageFilter_MatchTo); ok {
		return x.MatchTo
	}
	return ""
}

type isMessageFilter_Filter interface {
	isMessageFilter_Filter()
}

type MessageFilter_MatchFrom struct {
	MatchFrom string `protobuf:"bytes,1,opt,name=match_from,json=matchFrom,proto3,oneof"`
}

type MessageFilter_MatchTo struct {
	MatchTo string `protobuf:"bytes,2,opt,name=match_to,json=matchTo,proto3,oneof"`
}

func (*MessageFilter_MatchFrom) isMessageFilter_Filter() {}

func (*MessageFilter_MatchTo) isMessageFilter_Filter() {}

// A collection of message filters.
type MessageFilters struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Filters []*MessageFilter `protobuf:"bytes,1,rep,name=filters,proto3" json:"filters,omitempty"`
}

func (x *MessageFilters) Reset() {
	*x = MessageFilters{}
	if protoimpl.UnsafeEnabled {
		mi := &file_twisms_proto_msgTypes[4]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MessageFilters) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MessageFilters) ProtoMessage() {}

func (x *MessageFilters) ProtoReflect() protoreflect.Message {
	mi := &file_twisms_proto_msgTypes[4]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MessageFilters.ProtoReflect.Descriptor instead.
func (*MessageFilters) Descriptor() ([]byte, []int) {
	return file_twisms_proto_rawDescGZIP(), []int{4}
}

func (x *MessageFilters) GetFilters() []*MessageFilter {
	if x != nil {
		return x.Filters
	}
	return nil
}

var File_twisms_proto protoreflect.FileDescriptor

var file_twisms_proto_rawDesc = []byte{
	0x0a, 0x0c, 0x74, 0x77, 0x69, 0x73, 0x6d, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x06,
	0x74, 0x77, 0x69, 0x73, 0x6d, 0x73, 0x22, 0x56, 0x0a, 0x07, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67,
	0x65, 0x12, 0x12, 0x0a, 0x04, 0x66, 0x72, 0x6f, 0x6d, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x04, 0x66, 0x72, 0x6f, 0x6d, 0x12, 0x0e, 0x0a, 0x02, 0x74, 0x6f, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x09, 0x52, 0x02, 0x74, 0x6f, 0x12, 0x27, 0x0a, 0x04, 0x62, 0x6f, 0x64, 0x79, 0x18, 0x03, 0x20,
	0x01, 0x28, 0x0b, 0x32, 0x13, 0x2e, 0x74, 0x77, 0x69, 0x73, 0x6d, 0x73, 0x2e, 0x4d, 0x65, 0x73,
	0x73, 0x61, 0x67, 0x65, 0x42, 0x6f, 0x64, 0x79, 0x52, 0x04, 0x62, 0x6f, 0x64, 0x79, 0x22, 0x3d,
	0x0a, 0x0b, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x42, 0x6f, 0x64, 0x79, 0x12, 0x26, 0x0a,
	0x04, 0x74, 0x65, 0x78, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x10, 0x2e, 0x74, 0x77,
	0x69, 0x73, 0x6d, 0x73, 0x2e, 0x54, 0x65, 0x78, 0x74, 0x42, 0x6f, 0x64, 0x79, 0x48, 0x00, 0x52,
	0x04, 0x74, 0x65, 0x78, 0x74, 0x42, 0x06, 0x0a, 0x04, 0x62, 0x6f, 0x64, 0x79, 0x22, 0x1e, 0x0a,
	0x08, 0x54, 0x65, 0x78, 0x74, 0x42, 0x6f, 0x64, 0x79, 0x12, 0x12, 0x0a, 0x04, 0x74, 0x65, 0x78,
	0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04, 0x74, 0x65, 0x78, 0x74, 0x22, 0x57, 0x0a,
	0x0d, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x46, 0x69, 0x6c, 0x74, 0x65, 0x72, 0x12, 0x1f,
	0x0a, 0x0a, 0x6d, 0x61, 0x74, 0x63, 0x68, 0x5f, 0x66, 0x72, 0x6f, 0x6d, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x09, 0x48, 0x00, 0x52, 0x09, 0x6d, 0x61, 0x74, 0x63, 0x68, 0x46, 0x72, 0x6f, 0x6d, 0x12,
	0x1b, 0x0a, 0x08, 0x6d, 0x61, 0x74, 0x63, 0x68, 0x5f, 0x74, 0x6f, 0x18, 0x02, 0x20, 0x01, 0x28,
	0x09, 0x48, 0x00, 0x52, 0x07, 0x6d, 0x61, 0x74, 0x63, 0x68, 0x54, 0x6f, 0x42, 0x08, 0x0a, 0x06,
	0x66, 0x69, 0x6c, 0x74, 0x65, 0x72, 0x22, 0x41, 0x0a, 0x0e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67,
	0x65, 0x46, 0x69, 0x6c, 0x74, 0x65, 0x72, 0x73, 0x12, 0x2f, 0x0a, 0x07, 0x66, 0x69, 0x6c, 0x74,
	0x65, 0x72, 0x73, 0x18, 0x01, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x15, 0x2e, 0x74, 0x77, 0x69, 0x73,
	0x6d, 0x73, 0x2e, 0x4d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x46, 0x69, 0x6c, 0x74, 0x65, 0x72,
	0x52, 0x07, 0x66, 0x69, 0x6c, 0x74, 0x65, 0x72, 0x73, 0x42, 0x2e, 0x5a, 0x2c, 0x67, 0x69, 0x74,
	0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x74, 0x77, 0x69, 0x70, 0x69, 0x2f, 0x74, 0x77,
	0x69, 0x70, 0x69, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x6f, 0x75, 0x74, 0x2f, 0x74, 0x77,
	0x69, 0x73, 0x6d, 0x73, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x33,
}

var (
	file_twisms_proto_rawDescOnce sync.Once
	file_twisms_proto_rawDescData = file_twisms_proto_rawDesc
)

func file_twisms_proto_rawDescGZIP() []byte {
	file_twisms_proto_rawDescOnce.Do(func() {
		file_twisms_proto_rawDescData = protoimpl.X.CompressGZIP(file_twisms_proto_rawDescData)
	})
	return file_twisms_proto_rawDescData
}

var file_twisms_proto_msgTypes = make([]protoimpl.MessageInfo, 5)
var file_twisms_proto_goTypes = []interface{}{
	(*Message)(nil),        // 0: twisms.Message
	(*MessageBody)(nil),    // 1: twisms.MessageBody
	(*TextBody)(nil),       // 2: twisms.TextBody
	(*MessageFilter)(nil),  // 3: twisms.MessageFilter
	(*MessageFilters)(nil), // 4: twisms.MessageFilters
}
var file_twisms_proto_depIdxs = []int32{
	1, // 0: twisms.Message.body:type_name -> twisms.MessageBody
	2, // 1: twisms.MessageBody.text:type_name -> twisms.TextBody
	3, // 2: twisms.MessageFilters.filters:type_name -> twisms.MessageFilter
	3, // [3:3] is the sub-list for method output_type
	3, // [3:3] is the sub-list for method input_type
	3, // [3:3] is the sub-list for extension type_name
	3, // [3:3] is the sub-list for extension extendee
	0, // [0:3] is the sub-list for field type_name
}

func init() { file_twisms_proto_init() }
func file_twisms_proto_init() {
	if File_twisms_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_twisms_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Message); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_twisms_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MessageBody); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_twisms_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*TextBody); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_twisms_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MessageFilter); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_twisms_proto_msgTypes[4].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MessageFilters); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	file_twisms_proto_msgTypes[1].OneofWrappers = []interface{}{
		(*MessageBody_Text)(nil),
	}
	file_twisms_proto_msgTypes[3].OneofWrappers = []interface{}{
		(*MessageFilter_MatchFrom)(nil),
		(*MessageFilter_MatchTo)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_twisms_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   5,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_twisms_proto_goTypes,
		DependencyIndexes: file_twisms_proto_depIdxs,
		MessageInfos:      file_twisms_proto_msgTypes,
	}.Build()
	File_twisms_proto = out.File
	file_twisms_proto_rawDesc = nil
	file_twisms_proto_goTypes = nil
	file_twisms_proto_depIdxs = nil
}
