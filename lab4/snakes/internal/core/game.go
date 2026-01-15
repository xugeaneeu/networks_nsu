package core

import (
	"math/rand"
	pb "networks_nsu/lab4/proto"
)

type Game struct {
	Config *pb.GameConfig
	State  *pb.GameState
	Grid   *Grid

	// Буфер ввода: ID игрока -> Направление, которое он нажал
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

// ApplySteer сохраняет желание игрока повернуть. Применится на следующем Step().
func (g *Game) ApplySteer(playerID int32, dir pb.Direction) {
	// Базовая валидация (нельзя развернуться на 180 градусов сразу) будет внутри Step,
	// но сохраняем мы последнее нажатие.
	g.steerBuffer[playerID] = dir
}

// Step выполняет один тик симуляции
func (g *Game) Step() {
	// 1. Движение всех змей
	g.moveSnakes()

	// 2. Проверка столкновений
	g.checkCollisions()

	// 3. Поедание еды
	g.eatFood()

	// 4. Генерация новой еды
	g.spawnFood()

	// 5. Обновление номера состояния
	g.State.StateOrder++
}

// AddPlayer добавляет игрока (возвращает ID)
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

// RemovePlayer удаляет игрока (превращает змею в Зомби)
func (g *Game) RemovePlayer(id int32) {
	// Удаляем из списка игроков
	players := g.State.Players.Players
	for i, p := range players {
		if p.Id == id {
			g.State.Players.Players = append(players[:i], players[i+1:]...)
			break
		}
	}

	// Змею делаем зомби
	for _, snake := range g.State.Snakes {
		if snake.PlayerId == id {
			snake.State = pb.GameState_Snake_ZOMBIE
		}
	}
}

// SetPlayerRole меняет роль игрока в стейте
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

// --- Приватная логика ---

func (g *Game) moveSnakes() {
	for _, snake := range g.State.Snakes {
		if snake.State != pb.GameState_Snake_ALIVE && snake.State != pb.GameState_Snake_ZOMBIE {
			continue
		}

		// Определяем направление
		currentDir := snake.HeadDirection
		if newDir, ok := g.steerBuffer[snake.PlayerId]; ok {
			// Проверка на разворот 180
			if !isOpposite(currentDir, newDir) {
				currentDir = newDir
			}
			// Очищаем буфер после применения (или нет? обычно ввод сбрасывается каждый тик)
			// В сетевой игре лучше держать, пока не придет новое, но тут тик.
		}
		snake.HeadDirection = currentDir

		// Движение:
		// Новая голова = Старая голова + Вектор направления
		head := snake.Points[0]
		dx, dy := dirToVec(currentDir)

		newHeadX, newHeadY := g.Grid.Wrap(head.X+dx, head.Y+dy)

		// Логика смещений в протоколе хитрая:
		// Points[0] = Absolute Coord
		// Points[1] = Offset from Head to 2nd point
		// Points[n] = Offset from (n-1) to n

		// Для простоты реализации "гусеницы":
		// 1. Создаем новую точку головы
		// 2. Вторая точка (бывшая голова) становится смещением относительно новой головы.
		// 3. Остальные точки сдвигаем/копируем.
		// 4. Последнюю точку (хвост) убираем (если не поели, но поедание обрабатывается отдельно).
		// Важно: в реализации протокола Snake - это набор "Key Points" (поворотов), а не всех клеток.
		// НО! В ТЗ сказано "последовательность клеток".
		// В примере реализации (`drawSnake`) они бежали по точкам:
		// `curX = curX + points[i].x`. Это значит, что Points хранит компактный путь (вектора).
		// Однако для Змейки (где каждый сегмент важен для коллизий) проще хранить
		// Points как "Вектора сегментов".

		// Давайте упростим задачу и будем хранить змею "поклеточно" в векторе Points,
		// где Points[0] - абсолют, а Points[1..N] - смещения к предыдущему сегменту.
		// Так было в примере кода (`snake.Points[i].X = snake.Points[i-1].X` - сдвиг массива).

		// Сдвигаем массив точек (как гусеницу)
		// Начинаем с конца, копируем предыдущий в текущий
		// Но точки - это смещения!
		// Это самая путаная часть legacy-кода.
		// Давайте перепишем на понятную: храним Абсолютные координаты всех точек внутри движка,
		// а при отправке по сети конвертируем в "смещения", если того требует протокол.
		// А СТОП. Протокол требует `repeated Coord points`.
		// ТЗ: "Первая точка хранит координаты головы... Каждая следующая - смещение...".

		// Реализуем "Сдвиг массива смещений" (Legacy logic, but cleaned up):

		// 1. Вычисляем новую координату головы
		// (newHeadX, newHeadY)

		// 2. Вычисляем смещение от НОВОЙ головы к СТАРОЙ голове
		// diffX = oldHead.x - newHead.x
		diffX := head.X - newHeadX
		diffY := head.Y - newHeadY

		// Коррекция тора для смещения
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

		// 3. Формируем новый массив точек
		newPoints := make([]*pb.GameState_Coord, len(snake.Points))
		newPoints[0] = &pb.GameState_Coord{X: newHeadX, Y: newHeadY}
		// Вторая точка теперь указывает на старую голову
		newPoints[1] = &pb.GameState_Coord{X: diffX, Y: diffY}

		// Копируем остальные хвосты
		for i := 2; i < len(snake.Points); i++ {
			newPoints[i] = snake.Points[i-1]
		}

		snake.Points = newPoints
	}

	// Очищаем буфер ввода после тика
	g.steerBuffer = make(map[int32]pb.Direction)
}

func (g *Game) checkCollisions() {
	// Карта занятых клеток: "x:y" -> список ID змей
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

		// Если в клетке головы больше 1 записи - значит столкновение
		// (Либо с другой змеей, либо с собой, либо 2 головы встретились)
		if len(occupants) > 1 {
			deadSnakes[snake.PlayerId] = true

			// Начисление очков убийце (тому, кто НЕ этот snake, но есть в этой клетке)
			for _, killerID := range occupants {
				if killerID != snake.PlayerId {
					g.addScore(killerID, 1)
				}
			}
		} else {
			// Проверка: есть ли в этой клетке ID этой змеи ДВАЖДЫ? (Укус себя за хвост)
			// (В `occupied` мы добавляли все сегменты. Если голова попала в тело, ID будет 2 раза)
			count := 0
			for _, id := range occupants {
				if id == snake.PlayerId {
					count++
				}
			}
			if count > 1 {
				deadSnakes[snake.PlayerId] = true
				// Очки за суицид не дают :)
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
				// Еда съедена одной змеей (или несколькими сразу), она исчезает
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

	// Простая попытка заспавнить (не гарантированная, чтобы не виснуть при полном поле)
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
	// Упрощенный спавн: ищем квадрат 5x5 рандомно
	// (Для краткости просто ищем свободное место, полную логику квадрата можно добавить позже)
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
	// Добавляем сегмент "в точку хвоста" (копия смещения 0,0)
	// При следующем ходе он "растянется"
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
