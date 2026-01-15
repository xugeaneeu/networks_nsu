package main

import (
	"context"
	"fmt"
	"log"

	"networks_nsu/lab4/internal/game"
	pb "networks_nsu/lab4/proto"

	rl "github.com/gen2brain/raylib-go/raylib"
)

const (
	CellSize = 20
)

// Состояния интерфейса
type AppState int

const (
	StateMenu AppState = iota
	StateGame
)

func main() {
	// 1. Конфиг по умолчанию (для создания игры)
	cfg := &pb.GameConfig{
		Width:        30,
		Height:       20,
		FoodStatic:   1,
		StateDelayMs: 100,
	}

	// 2. Инициализация Контроллера
	ctrl, err := game.NewController(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer ctrl.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctrl.Start(ctx) // Запускаем сеть сразу, чтобы ловить Анонсы в меню

	// 3. Инициализация Окна
	rl.InitWindow(800, 600, "Snake Game - Lobby")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)

	currentState := StateMenu

	for !rl.WindowShouldClose() {
		// --- UPDATE ---
		// Обрабатываем сеть и логику (если мы Мастер, тикаем игру)
		ctrl.Update()

		// Обработка ввода зависит от состояния
		if currentState == StateGame {
			if rl.IsKeyPressed(rl.KeyW) {
				ctrl.HandleInput(pb.Direction_UP)
			}
			if rl.IsKeyPressed(rl.KeyS) {
				ctrl.HandleInput(pb.Direction_DOWN)
			}
			if rl.IsKeyPressed(rl.KeyA) {
				ctrl.HandleInput(pb.Direction_LEFT)
			}
			if rl.IsKeyPressed(rl.KeyD) {
				ctrl.HandleInput(pb.Direction_RIGHT)
			}

			// Выход в меню по ESC (опционально)
			if rl.IsKeyPressed(rl.KeyEscape) {
				// Тут по-хорошему надо слать RoleChange(Viewer), но пока просто выходим в меню
				currentState = StateMenu
				rl.SetWindowTitle("Snake Game - Lobby")
			}
		}

		// --- DRAW ---
		rl.BeginDrawing()
		rl.ClearBackground(rl.Black)

		if currentState == StateMenu {
			drawMenu(ctrl, &currentState)
		} else {
			drawGame(ctrl, cfg)
		}

		rl.EndDrawing()
	}
}

// Отрисовка Меню (Лобби)
func drawMenu(ctrl *game.Controller, state *AppState) {
	centerX := int32(200)

	rl.DrawText("SNAKE MULTIPLAYER", centerX, 50, 40, rl.Green)
	rl.DrawText(fmt.Sprintf("My Addr: %s", ctrl.Net.GetLocalAddrString()), 10, 580, 10, rl.DarkGray)

	// Раздел 1: Создать игру
	rl.DrawText("Create New Game:", centerX, 130, 20, rl.White)
	rl.DrawText("[C] Start Host", centerX, 160, 20, rl.SkyBlue)

	if rl.IsKeyPressed(rl.KeyC) {
		ctrl.HostGame()
		*state = StateGame
		rl.SetWindowTitle("Snake - HOST")
	}

	// Раздел 2: Список игр
	rl.DrawText("Discovered Games:", centerX, 230, 20, rl.Yellow)

	// Превращаем мапу в список для сортировки/стабильности (в Go мапа рандомна)
	// Для простоты просто итерируем, порядок может скакать
	y := int32(260)
	i := 1

	// Если игр нет
	if len(ctrl.DiscoveredGames) == 0 {
		rl.DrawText("Searching for games...", centerX, y, 20, rl.Gray)
	}

	for addr, info := range ctrl.DiscoveredGames {
		// Формируем строку: "1. SuperGame (192.168.1.5:4000) [30x20]"
		text := fmt.Sprintf("[%d] %s (%s) - %dx%d players: %d",
			i,
			info.GameName,
			addr,
			info.Config.Width,
			info.Config.Height,
			len(info.Players.Players))

		rl.DrawText(text, centerX, y, 20, rl.White)

		// Обработка выбора (клавиши 1, 2, 3...)
		// KeyOne = 49. KeyNine = 57.
		key := rl.KeyOne + int32(i) - 1
		if rl.IsKeyPressed(key) {
			asViewer := rl.IsKeyDown(rl.KeyLeftShift) || rl.IsKeyDown(rl.KeyRightShift)
			ctrl.ConnectTo(addr, asViewer)
			*state = StateGame
			if asViewer {
				rl.SetWindowTitle("Snake - VIEWER")
			} else {
				rl.SetWindowTitle("Snake - CLIENT")
			}
		}

		y += 30
		i++
		if i > 9 {
			break
		} // Ограничим 9 играми
	}
}

// Отрисовка самой Игры
func drawGame(ctrl *game.Controller, cfg *pb.GameConfig) {
	// Если стейта еще нет (клиент не получил первый пакет), не падаем
	if ctrl.Core.State == nil {
		rl.DrawText("Connecting...", 300, 300, 30, rl.White)
		return
	}

	state := ctrl.Core.State

	// Еда
	for _, food := range state.Foods {
		drawCell(food.X, food.Y, rl.Red)
	}

	// Змеи
	for _, snake := range state.Snakes {
		color := rl.Green

		// 1. Свой или Чужой?
		if snake.PlayerId != ctrl.MyID {
			color = rl.Blue
		}

		// 2. Зомби? (Этот статус важнее, он перезаписывает цвет)
		if snake.State == pb.GameState_Snake_ZOMBIE {
			color = rl.Gray
		}

		coords := ctrl.Core.GetSnakeCoords(snake)
		for _, p := range coords {
			drawCell(p.X, p.Y, color)
		}
	}

	// HUD (Статус бар)
	// Очки
	myScore := int32(0)
	for _, p := range state.Players.Players {
		if p.Id == ctrl.MyID {
			myScore = p.Score
			break
		}
	}

	// Список игроков справа (если поле не во весь экран)
	// Но пока рисуем просто снизу
	statusText := fmt.Sprintf("Role: %v | ID: %d | Score: %d", ctrl.MyRole, ctrl.MyID, myScore)
	rl.DrawText(statusText, 10, int32(cfg.Height)*CellSize+10, 20, rl.White)
}

func drawCell(x, y int32, color rl.Color) {
	rl.DrawRectangle(x*CellSize, y*CellSize, CellSize-1, CellSize-1, color)
}
