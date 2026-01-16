package core

import (
	"math/rand"
	pb "networks_nsu/lab4/proto"
)

type Game struct {
	Config *pb.GameConfig
	State  *pb.GameState
	Grid   *Grid

	steerBuffer map[int32]pb.Direction
}

func NewGame(config *pb.GameConfig) *Game {
	return &Game{
		Config: config,
		Grid:   NewGrid(config.Width, config.Height),
		State: &pb.GameState{
			StateOrder: 0,
			Snakes:     make([]*pb.GameState_Snake, 0),
			Players:    &pb.GamePlayers{Players: make([]*pb.GamePlayer, 0)},
			Foods:      make([]*pb.GameState_Coord, 0),
		},
		steerBuffer: make(map[int32]pb.Direction),
	}
}

func (g *Game) ApplySteer(playerID int32, dir pb.Direction) {
	g.steerBuffer[playerID] = dir
}

func (g *Game) Step() {
	g.moveSnakes()
	g.checkCollisions()
	g.eatFood()
	g.spawnFood()
	g.State.StateOrder++
}

func (g *Game) AddPlayer(name string, role pb.NodeRole, ip string, port int32) int32 {
	newID := g.generateID()

	player := &pb.GamePlayer{
		Name:      name,
		Id:        newID,
		IpAddress: ip,
		Port:      port,
		Role:      role,
		Type:      pb.PlayerType_HUMAN,
		Score:     0,
	}

	g.State.Players.Players = append(g.State.Players.Players, player)

	if role != pb.NodeRole_VIEWER {
		g.spawnSnake(newID)
	}

	return newID
}

func (g *Game) RemovePlayer(id int32) {
	players := g.State.Players.Players
	for i, p := range players {
		if p.Id == id {
			g.State.Players.Players = append(players[:i], players[i+1:]...)
			break
		}
	}

	for _, snake := range g.State.Snakes {
		if snake.PlayerId == id {
			snake.State = pb.GameState_Snake_ZOMBIE
		}
	}
}

func (g *Game) SetPlayerRole(playerID int32, role pb.NodeRole) {
	for _, p := range g.State.Players.Players {
		if p.Id == playerID {
			p.Role = role
			break
		}
	}
}

func (g *Game) generateID() int32 {
	maxID := int32(0)
	for _, p := range g.State.Players.Players {
		if p.Id > maxID {
			maxID = p.Id
		}
	}
	return maxID + 1
}

func (g *Game) moveSnakes() {
	for _, snake := range g.State.Snakes {
		if snake.State != pb.GameState_Snake_ALIVE && snake.State != pb.GameState_Snake_ZOMBIE {
			continue
		}

		currentDir := snake.HeadDirection
		if newDir, ok := g.steerBuffer[snake.PlayerId]; ok {
			if !isOpposite(currentDir, newDir) {
				currentDir = newDir
			}
		}
		snake.HeadDirection = currentDir

		head := snake.Points[0]
		dx, dy := dirToVec(currentDir)

		newHeadX, newHeadY := g.Grid.Wrap(head.X+dx, head.Y+dy)

		diffX := head.X - newHeadX
		diffY := head.Y - newHeadY

		if diffX > 1 {
			diffX = -1
		}
		if diffX < -1 {
			diffX = 1
		}
		if diffY > 1 {
			diffY = -1
		}
		if diffY < -1 {
			diffY = 1
		}

		newPoints := make([]*pb.GameState_Coord, len(snake.Points))
		newPoints[0] = &pb.GameState_Coord{X: newHeadX, Y: newHeadY}
		newPoints[1] = &pb.GameState_Coord{X: diffX, Y: diffY}

		for i := 2; i < len(snake.Points); i++ {
			newPoints[i] = snake.Points[i-1]
		}

		snake.Points = newPoints
	}

	g.steerBuffer = make(map[int32]pb.Direction)
}

func (g *Game) checkCollisions() {
	occupied := make(map[int32][]int32)

	for _, snake := range g.State.Snakes {
		if snake.State == pb.GameState_Snake_ALIVE || snake.State == pb.GameState_Snake_ZOMBIE {
			coords := g.GetSnakeCoords(snake)
			for _, c := range coords {
				idx := c.Y*g.Grid.Width + c.X
				occupied[idx] = append(occupied[idx], snake.PlayerId)
			}
		}
	}

	deadSnakes := make(map[int32]bool)

	for _, snake := range g.State.Snakes {
		if snake.State != pb.GameState_Snake_ALIVE && snake.State != pb.GameState_Snake_ZOMBIE {
			continue
		}

		head := snake.Points[0]
		idx := head.Y*g.Grid.Width + head.X
		occupants := occupied[idx]

		if len(occupants) > 1 {
			deadSnakes[snake.PlayerId] = true

			for _, killerID := range occupants {
				if killerID != snake.PlayerId {
					g.addScore(killerID, 1)
				}
			}
		} else {
			count := 0
			for _, id := range occupants {
				if id == snake.PlayerId {
					count++
				}
			}
			if count > 1 {
				deadSnakes[snake.PlayerId] = true
			}
		}
	}

	for id := range deadSnakes {
		g.killSnake(id)
	}
}

func (g *Game) eatFood() {
	newFoods := make([]*pb.GameState_Coord, 0)

	for _, food := range g.State.Foods {
		eaten := false
		for _, snake := range g.State.Snakes {
			if snake.State != pb.GameState_Snake_ALIVE && snake.State != pb.GameState_Snake_ZOMBIE {
				continue
			}
			head := snake.Points[0]
			if head.X == food.X && head.Y == food.Y {
				eaten = true
				g.growSnake(snake)
				g.addScore(snake.PlayerId, 1)
			}
		}
		if !eaten {
			newFoods = append(newFoods, food)
		}
	}
	g.State.Foods = newFoods
}

func (g *Game) spawnFood() {
	aliveCount := 0
	for _, s := range g.State.Snakes {
		if s.State == pb.GameState_Snake_ALIVE {
			aliveCount++
		}
	}
	target := int(g.Config.FoodStatic) + aliveCount
	needed := target - len(g.State.Foods)

	if needed <= 0 {
		return
	}

	for i := 0; i < needed; i++ {
		for attempt := 0; attempt < 20; attempt++ {
			x := rand.Int31n(g.Grid.Width)
			y := rand.Int31n(g.Grid.Height)
			if !g.isCellOccupied(x, y) {
				g.State.Foods = append(g.State.Foods, &pb.GameState_Coord{X: x, Y: y})
				break
			}
		}
	}
}

// --- Helpers ---

func (g *Game) spawnSnake(playerID int32) {
	for attempt := 0; attempt < 50; attempt++ {
		x := rand.Int31n(g.Grid.Width)
		y := rand.Int31n(g.Grid.Height)

		// Нужно место для головы и хвоста
		if !g.isCellOccupied(x, y) {
			// Спавним змею длиной 2
			snake := &pb.GameState_Snake{
				PlayerId:      playerID,
				HeadDirection: pb.Direction_RIGHT,
				State:         pb.GameState_Snake_ALIVE,
				Points: []*pb.GameState_Coord{
					{X: x, Y: y},
					{X: -1, Y: 0}, // Хвост слева
				},
			}
			g.State.Snakes = append(g.State.Snakes, snake)
			return
		}
	}
}

func (g *Game) killSnake(id int32) {
	for i, s := range g.State.Snakes {
		if s.PlayerId == id {
			// Превращаем в еду (50% шанс)
			coords := g.GetSnakeCoords(s)
			for _, c := range coords {
				if rand.Float32() < 0.5 {
					g.State.Foods = append(g.State.Foods, &pb.GameState_Coord{X: c.X, Y: c.Y})
				}
			}
			// Удаляем змею
			g.State.Snakes = append(g.State.Snakes[:i], g.State.Snakes[i+1:]...)
			return
		}
	}
}

func (g *Game) growSnake(snake *pb.GameState_Snake) {
	snake.Points = append(snake.Points, &pb.GameState_Coord{X: 0, Y: 0})
}

func (g *Game) addScore(playerID int32, score int32) {
	for _, p := range g.State.Players.Players {
		if p.Id == playerID {
			p.Score += score
		}
	}
}

// getSnakeCoords разворачивает относительные координаты в абсолютные
func (g *Game) GetSnakeCoords(snake *pb.GameState_Snake) []*pb.GameState_Coord {
	res := make([]*pb.GameState_Coord, 0, len(snake.Points))
	if len(snake.Points) == 0 {
		return res
	}

	currX, currY := snake.Points[0].X, snake.Points[0].Y
	res = append(res, &pb.GameState_Coord{X: currX, Y: currY})

	for i := 1; i < len(snake.Points); i++ {
		currX, currY = g.Grid.Wrap(currX+snake.Points[i].X, currY+snake.Points[i].Y)
		res = append(res, &pb.GameState_Coord{X: currX, Y: currY})
	}
	return res
}

func (g *Game) isCellOccupied(x, y int32) bool {
	// Проверка еды
	for _, f := range g.State.Foods {
		if f.X == x && f.Y == y {
			return true
		}
	}
	// Проверка змей
	for _, s := range g.State.Snakes {
		coords := g.GetSnakeCoords(s)
		for _, c := range coords {
			if c.X == x && c.Y == y {
				return true
			}
		}
	}
	return false
}

func isOpposite(d1, d2 pb.Direction) bool {
	switch d1 {
	case pb.Direction_UP:
		return d2 == pb.Direction_DOWN
	case pb.Direction_DOWN:
		return d2 == pb.Direction_UP
	case pb.Direction_LEFT:
		return d2 == pb.Direction_RIGHT
	case pb.Direction_RIGHT:
		return d2 == pb.Direction_LEFT
	}
	return false
}

func dirToVec(d pb.Direction) (int32, int32) {
	switch d {
	case pb.Direction_UP:
		return 0, -1
	case pb.Direction_DOWN:
		return 0, 1
	case pb.Direction_LEFT:
		return -1, 0
	case pb.Direction_RIGHT:
		return 1, 0
	}
	return 0, 0
}
