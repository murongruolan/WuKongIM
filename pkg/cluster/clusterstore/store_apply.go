package clusterstore

import (
	"context"
	"time"

	"github.com/WuKongIM/WuKongIM/pkg/cluster/replica"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// 频道元数据应用
func (s *Store) OnMetaApply(slotId uint32, logs []replica.Log) error {

	timeoutCtx, cancel := context.WithTimeout(s.ctx, time.Minute*2)
	defer cancel()
	requestGroup, _ := errgroup.WithContext(timeoutCtx)
	requestGroup.SetLimit(400) // 同时应用的并发数
	for _, lg := range logs {
		requestGroup.Go(func(l replica.Log) func() error {
			return func() error {
				return s.onMetaApply(slotId, l)
			}
		}(lg))
	}
	return requestGroup.Wait()
}

func (s *Store) onMetaApply(slotId uint32, log replica.Log) error {
	cmd := &CMD{}
	err := cmd.Unmarshal(log.Data)
	if err != nil {
		s.Error("unmarshal cmd err", zap.Error(err), zap.Uint32("slotId", slotId), zap.Uint64("index", log.Index), zap.ByteString("data", log.Data))
		return err
	}

	start := time.Now()
	defer func() {
		end := time.Since(start)
		if end > time.Millisecond*500 {
			s.Info("meta apply", zap.Duration("cost", end), zap.Uint32("slotId", slotId), zap.String("cmdType", cmd.CmdType.String()), zap.Int("dataLen", len(cmd.Data)))
		}
	}()
	err = s.execCMD(cmd)
	if err != nil {
		s.Error("exec cmd err", zap.Error(err), zap.String("cmdType", cmd.CmdType.String()), zap.Uint32("slotId", slotId), zap.Uint64("index", log.Index), zap.ByteString("data", log.Data))
		return err
	}
	return nil
}

func (s *Store) execCMD(cmd *CMD) error {
	switch cmd.CmdType {
	case CMDAddSubscribers: // 添加订阅者
		return s.handleAddSubscribers(cmd)
	case CMDRemoveSubscribers: // 移除订阅者
		return s.handleRemoveSubscribers(cmd)
	case CMDAddUser: // 添加用户
		return s.handleAddUser(cmd)
	case CMDUpdateUser: // 更新用户
		return s.handleUpdateUser(cmd)
	case CMDAddDevice: // 添加设备信息
		return s.handleAddDevice(cmd)
	case CMDUpdateDevice: // 更新设备信息
		return s.handleUpdateDevice(cmd)
	case CMDAddChannelInfo: // 添加频道信息
		return s.handleAddChannelInfo(cmd)
	case CMDUpdateChannelInfo: // 更新频道信息
		return s.handleUpdateChannel(cmd)
	case CMDRemoveAllSubscriber: // 移除所有订阅者
		return s.handleRemoveAllSubscriber(cmd)
	case CMDDeleteChannel: // 删除频道
		return s.handleDeleteChannel(cmd)
	case CMDAddDenylist: // 添加黑名单
		return s.handleAddDenylist(cmd)
	case CMDRemoveDenylist: // 移除黑名单
		return s.handleRemoveDenylist(cmd)
	case CMDRemoveAllDenylist: // 移除所有黑名单
		return s.handleRemoveAllDenylist(cmd)
	case CMDAddAllowlist: // 添加白名单
		return s.handleAddAllowlist(cmd)
	case CMDRemoveAllowlist: // 移除白名单
		return s.handleRemoveAllowlist(cmd)
	case CMDRemoveAllAllowlist: // 移除所有白名单
		return s.handleRemoveAllAllowlist(cmd)
	case CMDAddOrUpdateConversations: // 添加或更新会话
		return s.handleAddOrUpdateConversations(cmd)
	case CMDDeleteConversation: // 删除会话
		return s.handleDeleteConversation(cmd)
	case CMDDeleteConversations: // 批量删除某个用户的最近会话
		return s.handleDeleteConversations(cmd)
	case CMDChannelClusterConfigSave: // 保存频道分布式配置
		return s.handleChannelClusterConfigSave(cmd)
	// case CMDAppendMessagesOfUser: // 向用户队列里增加消息
	// 	return s.handleAppendMessagesOfUser(cmd)
	case CMDBatchUpdateConversation:
		return s.handleBatchUpdateConversation(cmd)
		// case CMDChannelClusterConfigDelete: // 删除频道分布式配置
		// return s.handleChannelClusterConfigDelete(cmd)
	case CMDSystemUIDsAdd: // 添加系统UID
		return s.handleSystemUIDsAdd(cmd)
	case CMDSystemUIDsRemove: // 移除系统UID
		return s.handleSystemUIDsRemove(cmd)
	case CMDAddStreamMeta: // 添加流元数据
		return s.handleAddStreamMeta(cmd)
	case CMDAddStreams: // 添加流
		return s.handleAddStreams(cmd)

	}
	return nil
}

func (s *Store) handleAddSubscribers(cmd *CMD) error {
	channelId, channelType, members, err := cmd.DecodeMembers()
	if err != nil {
		s.Error("decode subscribers err", zap.Error(err), zap.String("channelID", channelId), zap.Uint8("channelType", channelType), zap.ByteString("data", cmd.Data))
		return err
	}
	return s.wdb.AddSubscribers(channelId, channelType, members)
}

func (s *Store) handleRemoveSubscribers(cmd *CMD) error {
	channelId, channelType, subscribers, err := cmd.DecodeChannelUids()
	if err != nil {
		return err
	}
	return s.wdb.RemoveSubscribers(channelId, channelType, subscribers)
}

func (s *Store) handleAddUser(cmd *CMD) error {
	u, err := cmd.DecodeCMDUser()
	if err != nil {
		return err
	}
	return s.wdb.AddUser(u)
}

func (s *Store) handleUpdateUser(cmd *CMD) error {
	u, err := cmd.DecodeCMDUser()
	if err != nil {
		return err
	}
	return s.wdb.UpdateUser(u)
}

func (s *Store) handleAddDevice(cmd *CMD) error {
	u, err := cmd.DecodeCMDDevice()
	if err != nil {
		return err
	}
	return s.wdb.AddDevice(u)
}

func (s *Store) handleUpdateDevice(cmd *CMD) error {
	u, err := cmd.DecodeCMDDevice()
	if err != nil {
		return err
	}
	return s.wdb.UpdateDevice(u)
}

func (s *Store) handleAddChannelInfo(cmd *CMD) error {
	channelInfo, err := cmd.DecodeChannelInfo()
	if err != nil {
		return err
	}
	_, err = s.wdb.AddChannel(channelInfo)
	return err
}

func (s *Store) handleUpdateChannel(cmd *CMD) error {
	channelInfo, err := cmd.DecodeChannelInfo()
	if err != nil {
		return err
	}
	err = s.wdb.UpdateChannel(channelInfo)
	return err
}

func (s *Store) handleRemoveAllSubscriber(cmd *CMD) error {
	channelId, channelType, err := cmd.DecodeChannel()
	if err != nil {
		return err
	}
	return s.wdb.RemoveAllSubscriber(channelId, channelType)
}

func (s *Store) handleDeleteChannel(cmd *CMD) error {
	channelId, channelType, err := cmd.DecodeChannel()
	if err != nil {
		return err
	}
	return s.wdb.DeleteChannel(channelId, channelType)
}

func (s *Store) handleAddDenylist(cmd *CMD) error {
	channelId, channelType, members, err := cmd.DecodeMembers()
	if err != nil {
		return err
	}
	return s.wdb.AddDenylist(channelId, channelType, members)
}

func (s *Store) handleRemoveDenylist(cmd *CMD) error {
	channelId, channelType, subscribers, err := cmd.DecodeChannelUids()
	if err != nil {
		return err
	}
	return s.wdb.RemoveDenylist(channelId, channelType, subscribers)
}

func (s *Store) handleRemoveAllDenylist(cmd *CMD) error {
	channelId, channelType, err := cmd.DecodeChannel()
	if err != nil {
		return err
	}
	return s.wdb.RemoveAllDenylist(channelId, channelType)
}

func (s *Store) handleAddAllowlist(cmd *CMD) error {
	channelId, channelType, subscribers, err := cmd.DecodeMembers()
	if err != nil {
		return err
	}
	return s.wdb.AddAllowlist(channelId, channelType, subscribers)
}

func (s *Store) handleRemoveAllowlist(cmd *CMD) error {
	channelId, channelType, subscribers, err := cmd.DecodeChannelUids()
	if err != nil {
		return err
	}
	return s.wdb.RemoveAllowlist(channelId, channelType, subscribers)
}

func (s *Store) handleRemoveAllAllowlist(cmd *CMD) error {
	channelId, channelType, err := cmd.DecodeChannel()
	if err != nil {
		return err
	}
	return s.wdb.RemoveAllAllowlist(channelId, channelType)
}

func (s *Store) handleAddOrUpdateConversations(cmd *CMD) error {
	uid, conversations, err := cmd.DecodeCMDAddOrUpdateConversations()
	if err != nil {
		return err
	}
	return s.wdb.AddOrUpdateConversations(uid, conversations)
}

func (s *Store) handleDeleteConversation(cmd *CMD) error {
	uid, deleteChannelID, deleteChannelType, err := cmd.DecodeCMDDeleteConversation()
	if err != nil {
		return err
	}
	return s.wdb.DeleteConversation(uid, deleteChannelID, deleteChannelType)
}

func (s *Store) handleDeleteConversations(cmd *CMD) error {
	uid, channels, err := cmd.DecodeCMDDeleteConversations()
	if err != nil {
		return err
	}
	return s.wdb.DeleteConversations(uid, channels)
}

func (s *Store) handleChannelClusterConfigSave(cmd *CMD) error {
	_, _, configData, err := cmd.DecodeCMDChannelClusterConfigSave()
	if err != nil {
		return err
	}
	channelClusterConfig := wkdb.ChannelClusterConfig{}
	err = channelClusterConfig.Unmarshal(configData)
	if err != nil {
		return err
	}

	return s.wdb.SaveChannelClusterConfig(channelClusterConfig)
}

// func (s *Store) handleChannelClusterConfigDelete(cmd *CMD) error {
// 	channelId, channelType, err := cmd.DecodeChannel()
// 	if err != nil {
// 		return err
// 	}
// 	return s.db.DeleteChannelClusterConfig(channelId, channelType)
// }

func (s *Store) handleBatchUpdateConversation(cmd *CMD) error {
	models, err := cmd.DecodeCMDBatchUpdateConversation()
	if err != nil {
		return err
	}
	for _, model := range models {
		var conversationType = wkdb.ConversationTypeChat
		if s.opts.IsCmdChannel(model.ChannelId) {
			conversationType = wkdb.ConversationTypeCMD
		}
		for uid, seq := range model.Uids {
			conversation := wkdb.Conversation{
				Uid:          uid,
				Type:         conversationType,
				ChannelId:    model.ChannelId,
				ChannelType:  model.ChannelType,
				ReadToMsgSeq: seq,
			}
			err = s.wdb.AddOrUpdateConversations(uid, []wkdb.Conversation{conversation})
			if err != nil {
				return err
			}
		}

	}
	return nil
}

func (s *Store) handleSystemUIDsAdd(cmd *CMD) error {
	uids, err := cmd.DecodeCMDSystemUIDs()
	if err != nil {
		return err
	}
	return s.wdb.AddSystemUids(uids)
}

func (s *Store) handleSystemUIDsRemove(cmd *CMD) error {
	uids, err := cmd.DecodeCMDSystemUIDs()
	if err != nil {
		return err
	}
	return s.wdb.RemoveSystemUids(uids)
}

func (s *Store) handleAddStreamMeta(cmd *CMD) error {
	streamMeta, err := cmd.DecodeCMDAddStreamMeta()
	if err != nil {
		return err
	}
	return s.wdb.AddStreamMeta(streamMeta)
}

func (s *Store) handleAddStreams(cmd *CMD) error {
	streams, err := cmd.DecodeCMDAddStreams()
	if err != nil {
		return err
	}
	return s.wdb.AddStreams(streams)
}
