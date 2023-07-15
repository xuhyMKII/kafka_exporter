package tool

import (
	"crypto/sha256"
	"crypto/sha512"
	"hash"

	"github.com/xdg/scram"
)

//这个文件中的代码用于在kafka-exporter连接到需要身份验证的Kafka集群时进行身份验证。
//
//SASL/SCRAM是一种基于密码的身份验证机制，
//它可以提供密码的安全存储和安全传输。在Kafka中，
//可以配置Kafka集群以要求客户端进行身份验证，以增强安全性。
//
//当kafka-exporter需要连接到一个配置了SASL/SCRAM身份验证的Kafka集群时，
//它会使用scram_client.go中的代码来进行身份验证。
//这使得kafka-exporter可以安全地连接到并抓取需要身份验证的Kafka集群的信息。

var SHA256 scram.HashGeneratorFcn = func() hash.Hash { return sha256.New() }
var SHA512 scram.HashGeneratorFcn = func() hash.Hash { return sha512.New() }

type XDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = x.HashGeneratorFcn.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.Client.NewConversation()
	return nil
}

func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return
}

func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}
