package my_rpc

import (
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

type Foo int

type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

// it's not an exported Method
func (f Foo) sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

var s *service

func init() {
	var foo Foo
	s = newService(foo)
}

func TestNewService(t *testing.T) {
	ast := assert.New(t)

	ast.Equal(1, len(s.method))
	ast.NotNil(s.method["Sum"])
}

func TestMethodType_Call(t *testing.T) {
	ast := assert.New(t)

	mType := s.method["Sum"]

	argv := mType.newArgv()
	replyv := mType.newReplyv()
	argv.Set(reflect.ValueOf(Args{
		Num1: 10,
		Num2: 20,
	}))
	err := s.call(mType, argv, replyv)

	ast.Nil(err)
	ast.True(30 == *replyv.Interface().(*int))
	ast.True(1 == mType.NumCalls())
}
