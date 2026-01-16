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

type AppState int

const (
	StateMenu AppState = iota
	StateGame
)

func main() {
	cfg := &pb.GameConfig{
		Width:        30,
		Height:       20,
		FoodStatic:   1,
		StateDelayMs: 100,
	}

	ctrl, err := game.NewController(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer ctrl.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctrl.Start(ctx)

	rl.InitWindow(800, 600, "Snake Game - Lobby")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)

	currentState := StateMenu

	for !rl.WindowShouldClose() {
		ctrl.Update()

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

			if rl.IsKeyPressed(rl.KeyEscape) {
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

func drawMenu(ctrl *game.Controller, state *AppState) {
	centerX := int32(200)

	rl.DrawText("SNAKE MULTIPLAYER", centerX, 50, 40, rl.Green)
	rl.DrawText(fmt.Sprintf("My Addr: %s", ctrl.Net.GetLocalAddrString()), 10, 580, 10, rl.DarkGray)

	rl.DrawText("Create New Game:", centerX, 130, 20, rl.White)
	rl.DrawText("[C] Start Host", centerX, 160, 20, rl.SkyBlue)

	if rl.IsKeyPressed(rl.KeyC) {
		ctrl.HostGame()
		*state = StateGame
		rl.SetWindowTitle("Snake - HOST")
	}

	rl.DrawText("Discovered Games:", centerX, 230, 20, rl.Yellow)

	y := int32(260)
	i := 1

	if len(ctrl.DiscoveredGames) == 0 {
		rl.DrawText("Searching for games...", centerX, y, 20, rl.Gray)
	}

	for addr, info := range ctrl.DiscoveredGames {
		text := fmt.Sprintf("[%d] %s (%s) - %dx%d players: %d",
			i,
			info.GameName,
			addr,
			info.Config.Width,
			info.Config.Height,
			len(info.Players.Players))

		rl.DrawText(text, centerX, y, 20, rl.White)

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
		}
	}
}

func drawGame(ctrl *game.Controller, cfg *pb.GameConfig) {
	if ctrl.Core.State == nil {
		rl.DrawText("Connecting...", 300, 300, 30, rl.White)
		return
	}

	state := ctrl.Core.State

	for _, food := range state.Foods {
		drawCell(food.X, food.Y, rl.Red)
	}

	for _, snake := range state.Snakes {
		color := rl.Green

		if snake.PlayerId != ctrl.MyID {
			color = rl.Blue
		}

		if snake.State == pb.GameState_Snake_ZOMBIE {
			color = rl.Gray
		}

		coords := ctrl.Core.GetSnakeCoords(snake)
		for _, p := range coords {
			drawCell(p.X, p.Y, color)
		}
	}

	myScore := int32(0)
	for _, p := range state.Players.Players {
		if p.Id == ctrl.MyID {
			myScore = p.Score
			break
		}
	}

	statusText := fmt.Sprintf("Role: %v | ID: %d | Score: %d", ctrl.MyRole, ctrl.MyID, myScore)
	rl.DrawText(statusText, 10, int32(cfg.Height)*CellSize+10, 20, rl.White)
}

func drawCell(x, y int32, color rl.Color) {
	rl.DrawRectangle(x*CellSize, y*CellSize, CellSize-1, CellSize-1, color)
}
