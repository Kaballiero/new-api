package batchfixture

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// RedisReplies characterizes command bytes, return-code mapping and SQL acknowledgments.
// It does not execute Lua; the unchanged real Redis/miniredis regressions remain host evidence.
type RedisReplies struct {
	T       *testing.T
	Variant string
	Calls   [][]string
}

var redisReplyReady = errors.New("fixture reply supplied without a network connection")

func NewRedisReplies(t *testing.T, variant string) *RedisReplies {
	f := &RedisReplies{T: t, Variant: variant, Calls: [][]string{}}
	client := redis.NewClient(&redis.Options{Addr: "unused.invalid:0", MaxRetries: -1,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("Redis contract must never dial")
			return nil, errors.New("forbidden dial")
		}})
	client.AddHook(f)
	oldClient, oldEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = oldClient, oldEnabled; require.NoError(t, client.Close()) })
	return f
}

func (f *RedisReplies) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	args := make([]string, len(cmd.Args()))
	for i, value := range cmd.Args() {
		args[i] = fmt.Sprint(value)
	}
	f.Calls = append(f.Calls, args)
	return ctx, redisReplyReady
}

func (f *RedisReplies) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	t := f.T
	switch cmd.Name() {
	case "hgetall":
		key := fmt.Sprint(cmd.Args()[1])
		var value any
		if strings.HasPrefix(key, "user:") {
			var user model.User
			require.NoError(t, model.DB.Unscoped().First(&user, 1).Error)
			base := user.ToBaseUser()
			base.CacheSchema = 3
			if f.Variant == "auth_version" {
				base.AuthVersion++
			}
			if f.Variant == "unsafe_user_hold" {
				base.BatchHeldQuota++
			}
			value = base
		} else {
			var token model.Token
			require.NoError(t, model.DB.Unscoped().First(&token, 11).Error)
			token.CacheSchema = 1
			value = &token
		}
		fields := map[string]string{}
		v := reflect.ValueOf(value).Elem()
		typ := v.Type()
		for i := range v.NumField() {
			if v.Field(i).Kind() == reflect.Bool {
				fields[typ.Field(i).Name] = strconv.FormatBool(v.Field(i).Bool())
			} else if v.Field(i).Kind() != reflect.Pointer && typ.Field(i).Name != "DeletedAt" {
				fields[typ.Field(i).Name] = fmt.Sprint(v.Field(i).Interface())
			}
		}
		cmd.(*redis.StringStringMapCmd).SetVal(fields)
	case "mget":
		cmd.(*redis.SliceCmd).SetVal([]any{nil, nil})
	case "eval":
		if f.Variant == "redis_error" {
			cmd.SetErr(errors.New("fixture Redis failure"))
			return nil
		}
		result := int64(1)
		switch f.Variant {
		case "pending":
			result = 0
		case "unsafe_token":
			result = -1
		case "insufficient", "event_gap":
			result = -2
		}
		cmd.(*redis.Cmd).SetVal(result)
	case "del":
		cmd.(*redis.IntCmd).SetVal(1)
	default:
		t.Fatalf("undeclared Redis command %s", cmd.Name())
	}
	cmd.SetErr(nil)
	return nil
}

func (f *RedisReplies) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	f.T.Fatal("undeclared Redis pipeline")
	return ctx, redisReplyReady
}
func (f *RedisReplies) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }
