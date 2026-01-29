package test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	goredismq "github.com/Orfeo42/go-redismq/v2"
	"github.com/stretchr/testify/require"
)

func TestMethodInvoke(t *testing.T) {
	goredismq.RegisterRedisMqConfig(&goredismq.RedisMqConfig{
		Group:    TestGroup,
		Addr:     "127.0.0.1:6379",
		Password: "",
		Database: 0,
	})
	goredismq.RegisterListener(&TestListener{})
	goredismq.StartRedisMqConsumer()

	ctx := context.Background()
	goredismq.RegisterInvoke("TestInvoke", func(ctx context.Context, request interface{}) (response interface{}, err error) {
		if request == "error" {
			return nil, errors.New("error")
		} else if request == "panic" {
			panic("panic")
		} else if request == "timeout" {
			time.Sleep(30 * time.Second)

			return nil, errors.New("timeout")
		} else {
			return goredismq.MarshalToJsonString(request) + ":TestResponse", nil
		}
	})
	time.Sleep(5 * time.Second)
	t.Run("Test Method Invoke", func(t *testing.T) {
		res := goredismq.Invoke(ctx, &goredismq.InvoiceRequest{
			Group:   TestGroup,
			Method:  "TestInvoke",
			Request: 1,
		}, 0)
		require.NotNil(t, res)
		require.True(t, res.Status)
		fmt.Printf("TestRequest:%s\n", goredismq.MarshalToJsonString(res))
	})
	t.Run("Test Method Invoke Error", func(t *testing.T) {
		res := goredismq.Invoke(ctx, &goredismq.InvoiceRequest{
			Group:   TestGroup,
			Method:  "TestInvoke",
			Request: "error",
		}, 0)
		require.NotNil(t, res)
		require.False(t, res.Status)
		fmt.Printf("TestErrorRequest:%s\n", goredismq.MarshalToJsonString(res))
	})
	t.Run("Test Method Invoke Panic", func(t *testing.T) {
		res := goredismq.Invoke(ctx, &goredismq.InvoiceRequest{
			Group:   TestGroup,
			Method:  "TestInvoke",
			Request: "panic",
		}, 0)
		require.NotNil(t, res)
		require.False(t, res.Status)
		fmt.Printf("TestPanicRequest:%s\n", goredismq.MarshalToJsonString(res))
	})
	t.Run("Test Method Invoke Timeout", func(t *testing.T) {
		res := goredismq.Invoke(ctx, &goredismq.InvoiceRequest{
			Group:   TestGroup,
			Method:  "TestInvoke",
			Request: "timeout",
		}, 0)
		require.NotNil(t, res)
		require.False(t, res.Status)
		fmt.Printf("TestTimeOutRequest:%s\n", goredismq.MarshalToJsonString(res))
	})
}
