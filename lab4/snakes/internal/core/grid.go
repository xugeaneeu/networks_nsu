package core

type Grid struct {
	Width  int32
	Height int32
}

func NewGrid(width, height int32) *Grid {
	return &Grid{
		Width:  width,
		Height: height,
	}
}

func (g *Grid) Wrap(x, y int32) (int32, int32) {
	if x < 0 {
		x = g.Width + (x % g.Width)
	}
	x = x % g.Width

	if y < 0 {
		y = g.Height + (y % g.Height)
	}
	y = y % g.Height

	return x, y
}
