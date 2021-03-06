package database

import (
	"github.com/govm-net/govm/conf"
	"github.com/lengzhao/database/client"
)

var dfDB *client.Client

var clientNum int = 1

// GetClient get database client
func GetClient() *client.Client {
	if dfDB == nil {
		c := conf.GetConf()
		dfDB = client.New(c.DbAddrType, c.DbServerAddr, clientNum)
	}
	return dfDB
}

// ChangeClientNumber change client number
func ChangeClientNumber(in int) {
	if clientNum != in {
		clientNum = in
		c := conf.GetConf()
		dfDB = client.New(c.DbAddrType, c.DbServerAddr, clientNum)
	}
}
