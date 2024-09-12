package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/WuKongIM/WuKongIM/pkg/client"
	"github.com/WuKongIM/WuKongIM/pkg/cluster/clusterconfig/pb"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	"github.com/WuKongIM/WuKongIM/pkg/wkutil"
	wkproto "github.com/WuKongIM/WuKongIMGoProto"
	"github.com/stretchr/testify/assert"
)

func TestServerStart(t *testing.T) {
	s := NewTestServer(t)
	s.opts.Mode = TestMode
	err := s.Start()
	assert.Nil(t, err)
	err = s.Stop()
	assert.Nil(t, err)
}

// 测试单节点发送消息
func TestSingleSendMessage(t *testing.T) {
	s := NewTestServer(t)
	s.opts.Mode = TestMode
	err := s.Start()
	assert.Nil(t, err)
	defer s.StopNoErr()

	s.MustWaitClusterReady() // 等待服务准备好

	// new client 1
	cli1 := client.New(s.opts.External.TCPAddr, client.WithUID("test1"))
	err = cli1.Connect()
	assert.Nil(t, err)

	// new client 2
	cli2 := client.New(s.opts.External.TCPAddr, client.WithUID("test2"))
	err = cli2.Connect()
	assert.Nil(t, err)

	// send message
	err = cli1.SendMessage(client.NewChannel("test2", 1), []byte("hello"))
	assert.Nil(t, err)

	var wait sync.WaitGroup
	wait.Add(1)

	// cli2 recv
	cli2.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello", string(recv.Payload))
		wait.Done()
		return nil
	})

	wait.Wait()

}

func TestClusterSendMessage(t *testing.T) {
	s1, s2 := NewTestClusterServerTwoNode(t)
	err := s1.Start()
	assert.Nil(t, err)

	err = s2.Start()
	assert.Nil(t, err)

	defer s1.StopNoErr()
	defer s2.StopNoErr()

	MustWaitClusterReady(s1, s2)

	// new client 1
	cli1 := client.New(s1.opts.External.TCPAddr, client.WithUID("test1"))
	err = cli1.Connect()
	assert.Nil(t, err)

	// new client 2
	cli2 := client.New(s2.opts.External.TCPAddr, client.WithUID("test2"))
	err = cli2.Connect()
	assert.Nil(t, err)

	// send message to test2
	err = cli1.SendMessage(client.NewChannel("test2", 1), []byte("hello"))
	assert.Nil(t, err)

	var wait sync.WaitGroup
	wait.Add(1)

	// cli2 recv
	cli2.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello", string(recv.Payload))
		wait.Done()
		return nil
	})

	wait.Wait()
}

func TestClusterSlotMigrate(t *testing.T) {
	s1, s2 := NewTestClusterServerTwoNode(t)
	err := s1.Start()
	assert.Nil(t, err)

	err = s2.Start()
	assert.Nil(t, err)

	defer s1.StopNoErr()
	defer s2.StopNoErr()

	MustWaitClusterReady(s1, s2)

	leaderServer := GetLeaderServer(s1, s2)
	assert.NotNil(t, leaderServer)

	cfg := s1.GetClusterConfig()

	var migrateSlot *pb.Slot
	for _, slot := range cfg.Slots {
		if slot.Leader == s1.opts.Cluster.NodeId {
			migrateSlot = slot
			break
		}
	}
	assert.NotNil(t, migrateSlot)

	// 迁移slot
	err = leaderServer.MigrateSlot(migrateSlot.Id, s1.opts.Cluster.NodeId, s2.opts.Cluster.NodeId)
	assert.Nil(t, err)

	tk := time.NewTicker(time.Millisecond * 10)
	timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	for {
		select {
		case <-tk.C:

			cfg = s1.GetClusterConfig()
			for _, slot := range cfg.Slots {
				if slot.Id == migrateSlot.Id {
					if slot.Leader == s2.opts.Cluster.NodeId {
						return
					}
				}
			}

		case <-timeoutCtx.Done():
			assert.Nil(t, timeoutCtx.Err())
			return
		}
	}

}

// 槽迁移，从追随者迁移到leader
func TestClusterSlotMigrateForFollowToLeader(t *testing.T) {
	s1, s2 := NewTestClusterServerTwoNode(t, WithClusterSlotReplicaCount(2), WithClusterChannelReactorSubCount(1), WithClusterSlotReactorSubCount(1))
	err := s1.Start()
	assert.Nil(t, err)

	err = s2.Start()
	assert.Nil(t, err)

	defer s1.StopNoErr()
	defer s2.StopNoErr()

	MustWaitClusterReady(s1, s2)

	cfg := s1.GetClusterConfig()

	var migrateSlot *pb.Slot
	for _, slot := range cfg.Slots {
		if slot.Leader == s1.opts.Cluster.NodeId {
			migrateSlot = slot
			break
		}
	}

	err = s1.MigrateSlot(migrateSlot.Id, s1.opts.Cluster.NodeId, s2.opts.Cluster.NodeId)
	assert.Nil(t, err)

	tk := time.NewTicker(time.Millisecond * 10)
	timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	for {
		select {
		case <-tk.C:

			cfg = s1.GetClusterConfig()
			for _, slot := range cfg.Slots {
				if slot.Id == migrateSlot.Id {
					if slot.Leader == s2.opts.Cluster.NodeId {
						return
					}
				}
			}

		case <-timeoutCtx.Done():
			assert.Nil(t, timeoutCtx.Err())
			return
		}
	}

}

func TestClusterNodeJoin(t *testing.T) {
	s1, s2 := NewTestClusterServerTwoNode(t)
	err := s1.Start()
	assert.Nil(t, err)

	err = s2.Start()
	assert.Nil(t, err)

	defer s1.StopNoErr()
	defer s2.StopNoErr()

	MustWaitClusterReady(s1, s2)

	leaderServer := GetLeaderServer(s1, s2)
	assert.NotNil(t, leaderServer)

	cfg := s1.GetClusterConfig()
	assert.Equal(t, 2, len(cfg.Nodes))

	// new server
	s3 := NewTestServer(t, WithDemoOn(false), WithClusterSeed("1001@127.0.0.1:11110"), WithClusterServerAddr("0.0.0.0:11115"), WithWSAddr("ws://0.0.0.0:5250"), WithManagerAddr("0.0.0.0:5350"), WithAddr("tcp://0.0.0.0:5150"), WithHTTPAddr("0.0.0.0:5005"), WithClusterAddr("tcp://0.0.0.0:11115"), WithClusterNodeId(1005))
	err = s3.Start()
	assert.Nil(t, err)
	defer s3.StopNoErr()

	tk := time.NewTicker(time.Millisecond * 10)
	timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	for {
		select {
		case <-tk.C:
			cfg = s3.GetClusterConfig()
			existLearner := false
			existReplica := false
			for _, slot := range cfg.Slots {
				if wkutil.ArrayContainsUint64(slot.Learners, s3.opts.Cluster.NodeId) {
					existLearner = true
					break
				}
				if wkutil.ArrayContainsUint64(slot.Replicas, s3.opts.Cluster.NodeId) {
					existReplica = true
					break
				}
			}
			if !existLearner && existReplica {
				cfg = s1.GetClusterConfig()
				time.Sleep(time.Second * 1)
				return
			}

		case <-timeoutCtx.Done():
			assert.Nil(t, timeoutCtx.Err())
			return
		}
	}

}

func TestClusterChannelMigrate(t *testing.T) {
	s1, s2 := NewTestClusterServerTwoNode(t, WithClusterChannelReplicaCount(1), WithClusterSlotReplicaCount(2))
	err := s1.Start()
	assert.Nil(t, err)

	err = s2.Start()
	assert.Nil(t, err)

	defer s1.StopNoErr()
	defer s2.StopNoErr()

	MustWaitClusterReady(s1, s2)

	// new client 1
	cli1 := client.New(s1.opts.External.TCPAddr, client.WithUID("test1"))
	err = cli1.Connect()
	assert.Nil(t, err)

	// new client 2
	cli2 := client.New(s2.opts.External.TCPAddr, client.WithUID("test2"))
	err = cli2.Connect()
	assert.Nil(t, err)

	// send message to test2
	err = cli1.SendMessage(client.NewChannel("test2", 1), []byte("hello"))
	assert.Nil(t, err)

	var wait sync.WaitGroup
	wait.Add(1)

	// cli2 recv
	cli2.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello", string(recv.Payload))
		wait.Done()
		return nil
	})

	wait.Wait()

	cfg, err := s1.store.DB().GetChannelClusterConfig("test1@test2", 1)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(cfg.Replicas))

	// 迁移到另外一个节点

	var migrateTo uint64
	if cfg.Replicas[0] == s1.opts.Cluster.NodeId {
		migrateTo = s2.opts.Cluster.NodeId
	} else {
		migrateTo = s1.opts.Cluster.NodeId
	}
	cfg.MigrateFrom = cfg.Replicas[0]
	cfg.MigrateTo = migrateTo
	cfg.Learners = append(cfg.Learners, migrateTo)

	timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	err = s1.clusterServer.ProposeChannelClusterConfig(timeoutCtx, cfg)
	assert.Nil(t, err)

	s1.clusterServer.UpdateChannelClusterConfig(cfg)
	s2.clusterServer.UpdateChannelClusterConfig(cfg)

	time.Sleep(time.Second * 1)

}

func TestClusterChannelElection(t *testing.T) {
	s1, s2, s3 := NewTestClusterServerTreeNode(t)

	err := s1.Start()
	assert.Nil(t, err)

	err = s2.Start()
	assert.Nil(t, err)

	err = s3.Start()
	assert.Nil(t, err)

	MustWaitClusterReady(s1, s2, s3)

	// new client 1
	cli1 := client.New(s1.opts.External.TCPAddr, client.WithUID("test1"))
	err = cli1.Connect()
	assert.Nil(t, err)

	// new client 2
	cli2 := client.New(s2.opts.External.TCPAddr, client.WithUID("test2"))
	err = cli2.Connect()
	assert.Nil(t, err)

	// send message to test2
	err = cli1.SendMessage(client.NewChannel("test2", 1), []byte("hello"))
	assert.Nil(t, err)

	var wait sync.WaitGroup
	wait.Add(1)

	// cli2 recv
	cli2.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello", string(recv.Payload))
		wait.Done()
		return nil
	})

	wait.Wait()

	fakeChannelId := "test1@test2"

	slotLeaderId, err := s1.clusterServer.SlotLeaderIdOfChannel(fakeChannelId, 1)
	assert.Nil(t, err)

	// 获得槽领导的server
	var slotLeaderServer *Server
	switch slotLeaderId {
	case s1.opts.Cluster.NodeId:
		slotLeaderServer = s1
	case s2.opts.Cluster.NodeId:
		slotLeaderServer = s2
	case s3.opts.Cluster.NodeId:
		slotLeaderServer = s3
	}
	assert.NotNil(t, slotLeaderServer)

	node, err := slotLeaderServer.clusterServer.LeaderOfChannelForRead(fakeChannelId, 1)
	assert.Nil(t, err)

	// 获得channel领导的server
	var channelServer *Server
	switch node.Id {
	case s1.opts.Cluster.NodeId:
		channelServer = s1
	case s2.opts.Cluster.NodeId:
		channelServer = s2
	case s3.opts.Cluster.NodeId:
		channelServer = s3
	}
	assert.NotNil(t, channelServer)

	// 关闭频道领导
	channelServer.StopNoErr()

	// 不是channelServer的server
	var notChannelServer *Server
	if s1.opts.Cluster.NodeId != channelServer.opts.Cluster.NodeId {
		notChannelServer = s1
	} else if s2.opts.Cluster.NodeId != channelServer.opts.Cluster.NodeId {
		notChannelServer = s2
	} else if s3.opts.Cluster.NodeId != channelServer.opts.Cluster.NodeId {
		notChannelServer = s3
	}
	// 等待离线
	notChannelServer.MustWaitNodeOffline(channelServer.opts.Cluster.NodeId)

	time.Sleep(time.Second * 1)

	// 重新连接非channelServer，然后发生消息
	cli1.Close()
	cli2.Close()

	wait = sync.WaitGroup{}
	wait.Add(1)

	cli1 = client.New(notChannelServer.opts.External.TCPAddr, client.WithUID("test1"))
	err = cli1.Connect()
	// assert.Nil(t, err)
	if err != nil {
		panic(err)
	}

	// new client 2
	cli2 = client.New(notChannelServer.opts.External.TCPAddr, client.WithUID("test2"))
	err = cli2.Connect()
	if err != nil {
		panic(err)
	}
	assert.Nil(t, err)

	err = cli1.SendMessage(client.NewChannel("test2", 1), []byte("hello"))
	assert.Nil(t, err)

	// cli2 recv
	cli2.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello", string(recv.Payload))
		wait.Done()
		return nil
	})

	wait.Wait()

	if s1.opts.Cluster.NodeId != channelServer.opts.Cluster.NodeId {
		s1.StopNoErr()
	}
	if s2.opts.Cluster.NodeId != channelServer.opts.Cluster.NodeId {
		s2.StopNoErr()
	}
	if s3.opts.Cluster.NodeId != channelServer.opts.Cluster.NodeId {
		s3.StopNoErr()
	}

}

// 测试故障转移
func TestClusterFailover(t *testing.T) {

	// 启动服务
	s1, s2, s3 := NewTestClusterServerTreeNode(t)
	err := s1.Start()
	assert.Nil(t, err)
	err = s2.Start()
	assert.Nil(t, err)
	err = s3.Start()
	assert.Nil(t, err)
	MustWaitClusterReady(s1, s2, s3)

	// 创建群频道
	channelId := "g1"
	channelType := wkproto.ChannelTypeGroup

	TestAddSubscriber(t, s2, channelId, channelType, "u1", "u2", "u3")

	cli1 := TestCreateClient(t, s1, "u1")
	cli2 := TestCreateClient(t, s2, "u2")
	cli3 := TestCreateClient(t, s3, "u3")

	// 发送消息
	err = cli1.SendMessage(client.NewChannel(channelId, channelType), []byte("hello"))
	assert.Nil(t, err)

	// 收消息
	var wait sync.WaitGroup
	wait.Add(2)

	cli2.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello", string(recv.Payload))
		wait.Done()
		return nil
	})

	cli3.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello", string(recv.Payload))
		wait.Done()
		return nil
	})

	wait.Wait()

	// 关闭服务器s3
	s3.StopNoErr()

	// 客户端cli3重连到服务器s2
	cli3.Close()
	cli3 = TestCreateClient(t, s2, "u3")

	// 重新监听消息
	wait.Add(2)
	cli2.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello2", string(recv.Payload))
		wait.Done()
		return nil
	})

	cli3.SetOnRecv(func(recv *wkproto.RecvPacket) error {
		assert.Equal(t, "hello2", string(recv.Payload))
		wait.Done()
		return nil
	})

	// 发送消息
	err = cli1.SendMessage(client.NewChannel(channelId, channelType), []byte("hello2"))
	assert.Nil(t, err)

	wait.Wait()

	s1.StopNoErr()
	s2.StopNoErr()

}

func TestClusterSaveClusterConfig(t *testing.T) {
	// 启动服务
	s1, s2, s3 := NewTestClusterServerTreeNode(t)

	TestStartServer(t, s1, s2, s3)

	MustWaitClusterReady(s1, s2, s3)

	defer s1.StopNoErr()
	defer s2.StopNoErr()
	defer s3.StopNoErr()

	createdAt := time.Now()
	updatedAt := time.Now()

	err := s1.store.SaveChannelClusterConfig(context.Background(), wkdb.ChannelClusterConfig{
		ChannelId:       "test1@test2",
		ChannelType:     1,
		ReplicaMaxCount: 3,
		Replicas:        []uint64{s1.opts.Cluster.NodeId, s2.opts.Cluster.NodeId, s3.opts.Cluster.NodeId},
		LeaderId:        1,
		Learners:        []uint64{},
		CreatedAt:       &createdAt,
		UpdatedAt:       &updatedAt,
	})
	assert.Nil(t, err)
}
