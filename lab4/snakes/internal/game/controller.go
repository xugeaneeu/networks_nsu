package game

import (
	"context"
	"log"
	"time"

	"networks_nsu/lab4/internal/core"
	"networks_nsu/lab4/internal/network"
	pb "networks_nsu/lab4/proto"
)

type Role int

const (
	RoleMaster Role = iota
	RoleNormal
	RoleDeputy
	RoleViewer
)

const (
	PingInterval = 100 * time.Millisecond
	NodeTimeout  = 2000 * time.Millisecond
)

type SentMsgInfo struct {
	Msg      *pb.GameMessage
	Addr     string
	LastSent time.Time
}

type PeerStatus struct {
	LastSeen time.Time
	LastSent time.Time
}

type Controller struct {
	Core *core.Game
	Net  *network.Manager

	MyRole     Role
	MyID       int32
	MasterAddr string

	lastStep  time.Time
	stepDelay time.Duration
	msgSeq    int64

	DiscoveredGames map[string]*pb.GameAnnouncement
	announceTicker  *time.Ticker

	unackedMessages map[int64]*SentMsgInfo
	ackTimeout      time.Duration

	peers              map[int32]*PeerStatus
	becomingMasterTime time.Time
}

func NewController(cfg *pb.GameConfig) (*Controller, error) {
	netMgr, err := network.NewManager()
	if err != nil {
		return nil, err
	}
	gameCore := core.NewGame(cfg)

	return &Controller{
		Core:            gameCore,
		Net:             netMgr,
		MyRole:          RoleNormal,
		stepDelay:       time.Duration(cfg.StateDelayMs) * time.Millisecond,
		lastStep:        time.Now(),
		DiscoveredGames: make(map[string]*pb.GameAnnouncement),
		announceTicker:  time.NewTicker(1 * time.Second),
		unackedMessages: make(map[int64]*SentMsgInfo),
		ackTimeout:      time.Duration(cfg.StateDelayMs) * time.Millisecond,
		peers:           make(map[int32]*PeerStatus),
	}, nil
}

func (c *Controller) Start(ctx context.Context) {
	c.Net.Start(ctx)
}

func (c *Controller) Shutdown() {
	c.Net.Close()
}

func (c *Controller) Update() {
	c.processResends()
	c.processNetworkMessages()
	c.checkTimeouts()
	c.keepAlive()

	if c.MyRole == RoleMaster {
		c.assignDeputyIfNeeded()

		if time.Since(c.lastStep) >= c.stepDelay {
			c.Core.Step()
			c.lastStep = time.Now()
			c.broadcastState()
		}

		select {
		case <-c.announceTicker.C:
			c.sendAnnouncement()
		default:
		}
	}
}

func (c *Controller) HandleInput(dir pb.Direction) {
	if c.MyRole == RoleViewer {
		return
	}
	if c.MyRole == RoleMaster {
		c.Core.ApplySteer(c.MyID, dir)
	} else {
		c.sendSteer(dir)
	}
}

// --- Network Processing ---

func (c *Controller) processNetworkMessages() {
	select {
	case msg := <-c.Net.Events():
		if msg.Payload.SenderId != 0 {
			if c.MyRole == RoleMaster {
			}
			c.touchPeer(msg.Payload.SenderId)
		}

		switch payload := msg.Payload.Type.(type) {
		case *pb.GameMessage_Ack:
			seq := msg.Payload.MsgSeq
			delete(c.unackedMessages, seq)
			if (c.MyRole == RoleNormal || c.MyRole == RoleViewer) && c.MyID == 0 {
				c.MyID = msg.Payload.ReceiverId
				c.touchPeer(msg.Payload.SenderId)
				log.Printf("Joined! My ID: %d", c.MyID)
			}

		case *pb.GameMessage_RoleChange:
			c.sendAck(msg.Payload.MsgSeq, msg.Payload.SenderId, msg.Addr.String())

			rc := payload.RoleChange

			if rc.ReceiverRole == pb.NodeRole_DEPUTY {
				log.Println("Promoted to DEPUTY")
				c.MyRole = RoleDeputy
			}

			if rc.SenderRole == pb.NodeRole_MASTER {
				log.Printf("New Master detected: %s (ID %d)", msg.Addr.String(), msg.Payload.SenderId)
				c.MasterAddr = msg.Addr.String()
				c.touchPeer(msg.Payload.SenderId)

				c.Core.SetPlayerRole(msg.Payload.SenderId, pb.NodeRole_MASTER)

				for _, p := range c.Core.State.Players.Players {
					if p.Role == pb.NodeRole_MASTER && p.Id != msg.Payload.SenderId {
						p.Role = pb.NodeRole_NORMAL
					}
				}
			}

		case *pb.GameMessage_Join:
			if c.MyRole == RoleMaster {
				c.handleJoin(msg.Addr.String(), payload.Join, msg.Payload.MsgSeq)
			}
		case *pb.GameMessage_Steer:
			if c.MyRole == RoleMaster {
				c.sendAck(msg.Payload.MsgSeq, msg.Payload.SenderId, msg.Addr.String())
				c.Core.ApplySteer(msg.Payload.SenderId, payload.Steer.Direction)
			}
		case *pb.GameMessage_State:
			if c.MyRole != RoleMaster {
				c.Core.State = payload.State.State
			}
		case *pb.GameMessage_Announcement:
			if c.MyRole == RoleMaster {
				break
			}
			if len(payload.Announcement.Games) > 0 {
				c.DiscoveredGames[msg.Addr.String()] = payload.Announcement.Games[0]
			}
		case *pb.GameMessage_Error:
			log.Printf("Error: %s", payload.Error.ErrorMessage)
		case *pb.GameMessage_Ping:
		}
	default:
	}
}

// --- Logic ---

func (c *Controller) checkTimeouts() {
	now := time.Now()

	// 1. ЛОГИКА МАСТЕРА
	if c.MyRole == RoleMaster {
		graceTime := c.becomingMasterTime.Add(NodeTimeout + 1*time.Second)
		if now.Before(graceTime) {
			return
		}

		players := c.Core.State.Players.Players
		playersCopy := make([]*pb.GamePlayer, len(players))
		copy(playersCopy, players)

		for _, p := range playersCopy {
			if p.Id == c.MyID {
				continue
			}

			if s, ok := c.peers[p.Id]; ok {
				delta := now.Sub(s.LastSeen)
				if delta > NodeTimeout {
					log.Printf("[DEBUG-TIMEOUT] Killing Player %d. LastSeen: %v ago. Threshold: %v", p.Id, delta, NodeTimeout)
					c.Core.RemovePlayer(p.Id)
					delete(c.peers, p.Id)
				}
			}
		}

	} else {
		// 2. ЛОГИКА КЛИЕНТА
		var masterID int32 = -1
		for _, p := range c.Core.State.Players.Players {
			if p.Role == pb.NodeRole_MASTER {
				masterID = p.Id
				break
			}
		}

		if masterID != -1 {
			if s, ok := c.peers[masterID]; ok && now.Sub(s.LastSeen) > NodeTimeout {
				if c.MyRole == RoleDeputy {
					log.Println("Master died. Taking over!")
					c.becomeMaster()
				} else {
					log.Println("Master died. Waiting...")
					delete(c.peers, masterID)
					c.MasterAddr = ""
				}
			}
		}
	}
}

func (c *Controller) keepAlive() {
	now := time.Now()
	ping := &pb.GameMessage{MsgSeq: c.nextSeq(), SenderId: c.MyID, Type: &pb.GameMessage_Ping{Ping: &pb.GameMessage_PingMsg{}}}

	if c.MyRole == RoleMaster {
		for _, p := range c.Core.State.Players.Players {
			if p.Id == c.MyID {
				continue
			}
			if s, ok := c.peers[p.Id]; ok && now.Sub(s.LastSent) > PingInterval {
				c.Net.SendUnicast(ping, p.IpAddress)
				c.markSent(p.Id)
			}
		}
	} else {
		if c.MasterAddr != "" {
			var masterID int32 = -1
			for _, p := range c.Core.State.Players.Players {
				if p.Role == pb.NodeRole_MASTER {
					masterID = p.Id
					break
				}
			}

			if masterID != -1 {
				status, known := c.peers[masterID]
				if !known {
					c.touchPeer(masterID)
					status = c.peers[masterID]
				}

				if now.Sub(status.LastSent) > PingInterval {
					c.Net.SendUnicast(ping, c.MasterAddr)
					c.markSent(masterID)
				}
			}
		}
	}
}

func (c *Controller) assignDeputyIfNeeded() {
	hasDeputy := false
	for _, p := range c.Core.State.Players.Players {
		if p.Role == pb.NodeRole_DEPUTY {
			hasDeputy = true
			break
		}
	}
	if hasDeputy {
		return
	}

	for _, p := range c.Core.State.Players.Players {
		if p.Role == pb.NodeRole_NORMAL && p.Id != c.MyID {
			log.Printf("Appointing Deputy: %d", p.Id)
			c.Core.SetPlayerRole(p.Id, pb.NodeRole_DEPUTY)

			msg := &pb.GameMessage{
				MsgSeq:     c.nextSeq(),
				SenderId:   c.MyID,
				ReceiverId: p.Id,
				Type: &pb.GameMessage_RoleChange{
					RoleChange: &pb.GameMessage_RoleChangeMsg{
						SenderRole:   pb.NodeRole_MASTER,
						ReceiverRole: pb.NodeRole_DEPUTY,
					},
				},
			}
			c.SendReliable(msg, p.IpAddress)
			return
		}
	}
}

func (c *Controller) becomeMaster() {
	c.MyRole = RoleMaster
	c.becomingMasterTime = time.Now()
	log.Printf("[DEBUG] BecomeMaster at %v. Grace period starts.", c.becomingMasterTime)

	// Сначала всех текущих мастеров в NORMAL
	// Чтобы в стейте гарантированно остался только один
	for _, p := range c.Core.State.Players.Players {
		if p.Role == pb.NodeRole_MASTER {
			p.Role = pb.NodeRole_NORMAL
		}
	}

	c.Core.SetPlayerRole(c.MyID, pb.NodeRole_MASTER)

	log.Println("I am the Master now. Old masters demoted.")

	msg := &pb.GameMessage{
		MsgSeq:   c.nextSeq(),
		SenderId: c.MyID,
		Type: &pb.GameMessage_RoleChange{
			RoleChange: &pb.GameMessage_RoleChangeMsg{
				SenderRole: pb.NodeRole_MASTER,
			},
		},
	}

	// Уведомляем всех живых игроков
	for _, p := range c.Core.State.Players.Players {
		if p.Id == c.MyID {
			continue
		}
		if p.IpAddress != "" {
			c.SendReliable(msg, p.IpAddress)
			c.touchPeer(p.Id)
		}
	}
}

func (c *Controller) handleJoin(addr string, joinMsg *pb.GameMessage_JoinMsg, originalSeq int64) {
	log.Printf("Player joining from %s: %s as %v", addr, joinMsg.PlayerName, joinMsg.RequestedRole)
	role := pb.NodeRole_NORMAL
	if joinMsg.RequestedRole == pb.NodeRole_VIEWER {
		role = pb.NodeRole_VIEWER
	}

	newID := c.Core.AddPlayer(joinMsg.PlayerName, role, addr, 0)
	c.touchPeer(newID)
	c.markSent(newID)

	ack := &pb.GameMessage{
		MsgSeq:     originalSeq,
		SenderId:   c.MyID,
		ReceiverId: newID,
		Type:       &pb.GameMessage_Ack{Ack: &pb.GameMessage_AckMsg{}},
	}
	c.Net.SendUnicast(ack, addr)
}

func (c *Controller) broadcastState() {
	stateMsg := &pb.GameMessage{
		MsgSeq:   c.nextSeq(),
		SenderId: c.MyID,
		Type:     &pb.GameMessage_State{State: &pb.GameMessage_StateMsg{State: c.Core.State}},
	}

	for _, p := range c.Core.State.Players.Players {
		if p.Id == c.MyID {
			continue
		}
		if p.IpAddress != "" {
			c.Net.SendUnicast(stateMsg, p.IpAddress)
			c.markSent(p.Id)
		}
	}
}

func (c *Controller) sendAnnouncement() {
	announce := &pb.GameMessage{
		MsgSeq:   c.nextSeq(),
		SenderId: c.MyID,
		Type: &pb.GameMessage_Announcement{
			Announcement: &pb.GameMessage_AnnouncementMsg{
				Games: []*pb.GameAnnouncement{{
					Players:  c.Core.State.Players,
					Config:   c.Core.Config,
					CanJoin:  true,
					GameName: "Snake Server",
				}},
			},
		},
	}
	c.Net.SendMulticast(announce)
}

func (c *Controller) processResends() {
	now := time.Now()
	for _, info := range c.unackedMessages {
		if now.Sub(info.LastSent) > c.ackTimeout {
			c.Net.SendUnicast(info.Msg, info.Addr)
			info.LastSent = now
		}
	}
}

func (c *Controller) ConnectTo(masterAddr string, asViewer bool) {
	c.MyRole = RoleNormal
	if asViewer {
		c.MyRole = RoleViewer
	}

	c.MasterAddr = masterAddr
	c.MyID = 0
	c.peers = make(map[int32]*PeerStatus)

	reqRole := pb.NodeRole_NORMAL
	if asViewer {
		reqRole = pb.NodeRole_VIEWER
	}

	join := &pb.GameMessage{
		MsgSeq: c.nextSeq(),
		Type: &pb.GameMessage_Join{
			Join: &pb.GameMessage_JoinMsg{
				PlayerName:    "Client",
				GameName:      "Snake",
				RequestedRole: reqRole,
			},
		},
	}
	c.SendReliable(join, masterAddr)
	log.Printf("Joining %s (Role: %v)...", masterAddr, reqRole)
}

func (c *Controller) sendSteer(dir pb.Direction) {
	if c.MasterAddr == "" {
		return
	}
	msg := &pb.GameMessage{
		MsgSeq:   c.nextSeq(),
		SenderId: c.MyID,
		Type:     &pb.GameMessage_Steer{Steer: &pb.GameMessage_SteerMsg{Direction: dir}},
	}
	c.SendReliable(msg, c.MasterAddr)

	for _, p := range c.Core.State.Players.Players {
		if p.Role == pb.NodeRole_MASTER {
			c.markSent(p.Id)
			break
		}
	}
}

func (c *Controller) SendReliable(msg *pb.GameMessage, addr string) {
	c.Net.SendUnicast(msg, addr)
	c.unackedMessages[msg.MsgSeq] = &SentMsgInfo{
		Msg:      msg,
		Addr:     addr,
		LastSent: time.Now(),
	}
}

func (c *Controller) sendAck(seq int64, receiverID int32, addr string) {
	ack := &pb.GameMessage{
		MsgSeq:     seq,
		SenderId:   c.MyID,
		ReceiverId: receiverID,
		Type:       &pb.GameMessage_Ack{Ack: &pb.GameMessage_AckMsg{}},
	}
	c.Net.SendUnicast(ack, addr)
	c.markSent(receiverID)
}

func (c *Controller) HostGame() {
	c.MyRole = RoleMaster
	c.peers = make(map[int32]*PeerStatus)
	c.becomingMasterTime = time.Now()
	c.MyID = c.Core.AddPlayer("Host", pb.NodeRole_MASTER, "localhost", 0)
	log.Printf("Hosting game. My ID: %d", c.MyID)
}

func (c *Controller) nextSeq() int64 {
	c.msgSeq++
	return c.msgSeq
}

func (c *Controller) touchPeer(id int32) {
	if _, ok := c.peers[id]; !ok {
		c.peers[id] = &PeerStatus{}
	}
	c.peers[id].LastSeen = time.Now()
}

func (c *Controller) markSent(id int32) {
	if _, ok := c.peers[id]; !ok {
		c.peers[id] = &PeerStatus{LastSeen: time.Now()}
	}
	c.peers[id].LastSent = time.Now()
}
