package go_redismq

import "github.com/redis/go-redis/v9"

var Group = "GID_Default"
var addr = ""
var password = ""
var database = 0

type RedisMqConfig struct {
	Group    string
	Addr     string
	Password string
	Database int
}

func RegisterRedisMqConfig(one *RedisMqConfig) {
	Assert(one != nil, "RegisterRedisMqConfig GetRedisStreamConfig nil")
	Assert(len(one.Addr) > 0, "RegisterRedisMqConfig Addr is blank")
	Assert(len(one.Group) > 0, "RegisterRedisMqConfig Group is blank")
	Group = one.Group
	addr = one.Addr

	password = one.Password
	if one.Database >= 0 {
		database = one.Database
	}
}

func GetRedisConfig() *redis.Options {
	if len(addr) == 0 {
		panic("Invalid redismq config, addr forgot setup?")
	}

	return &redis.Options{
		Addr:     addr,
		Password: password,
		DB:       database,
	}
}
