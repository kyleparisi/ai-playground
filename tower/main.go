package main

import (
	"fmt"
	"image/color"
	"log"
	"math/rand"
	"runtime"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/basicfont"
)

const (
	boardW = 10
	boardH = 20

	logicalW = 480
	logicalH = 640
)

var (
	bgColor     = color.RGBA{18, 18, 24, 255}
	gridColor   = color.RGBA{40, 40, 55, 255}
	ghostAlpha  = uint8(96)
	pieceColors = []color.RGBA{
		{0, 255, 255, 255}, // I
		{255, 255, 0, 255}, // O
		{160, 0, 240, 255}, // T
		{0, 200, 0, 255},   // S
		{220, 0, 0, 255},   // Z
		{0, 80, 220, 255},  // J
		{255, 140, 0, 255}, // L
	}
)

type point struct {
	x, y int
}

// pieceShapes[kind][rot] -> []point in a 4x4 bounding box
var pieceShapes = [7][4][]point{
	// I
	{
		{{0, 1}, {1, 1}, {2, 1}, {3, 1}},
		{{2, 0}, {2, 1}, {2, 2}, {2, 3}},
		{{0, 2}, {1, 2}, {2, 2}, {3, 2}},
		{{1, 0}, {1, 1}, {1, 2}, {1, 3}},
	},
	// O
	{
		{{1, 1}, {2, 1}, {1, 2}, {2, 2}},
		{{1, 1}, {2, 1}, {1, 2}, {2, 2}},
		{{1, 1}, {2, 1}, {1, 2}, {2, 2}},
		{{1, 1}, {2, 1}, {1, 2}, {2, 2}},
	},
	// T
	{
		{{1, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {1, 1}, {2, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {1, 2}},
		{{1, 0}, {0, 1}, {1, 1}, {1, 2}},
	},
	// S
	{
		{{1, 0}, {2, 0}, {0, 1}, {1, 1}},
		{{1, 0}, {1, 1}, {2, 1}, {2, 2}},
		{{1, 1}, {2, 1}, {0, 2}, {1, 2}},
		{{0, 0}, {0, 1}, {1, 1}, {1, 2}},
	},
	// Z
	{
		{{0, 0}, {1, 0}, {1, 1}, {2, 1}},
		{{2, 0}, {1, 1}, {2, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {1, 2}, {2, 2}},
		{{1, 0}, {0, 1}, {1, 1}, {0, 2}},
	},
	// J
	{
		{{0, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {2, 0}, {1, 1}, {1, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {2, 2}},
		{{1, 0}, {1, 1}, {0, 2}, {1, 2}},
	},
	// L
	{
		{{2, 0}, {0, 1}, {1, 1}, {2, 1}},
		{{1, 0}, {1, 1}, {1, 2}, {2, 2}},
		{{0, 1}, {1, 1}, {2, 1}, {0, 2}},
		{{0, 0}, {1, 0}, {1, 1}, {1, 2}},
	},
}

type activePiece struct {
	kind int
	rot  int
	x, y int
}

type Game struct {
	board            [boardH][boardW]int // 0 empty, 1..7 piece kinds
	cur              activePiece
	nextKind         int
	bag              []int
	rng              *rand.Rand
	score            int
	lines            int
	level            int
	dropFrameCounter int
	gameOver         bool
}

func NewGame() *Game {
	g := &Game{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	g.nextKind = g.popBag()
	g.spawn()
	return g
}

func (g *Game) Reset() {
	*g = *NewGame()
}

func (g *Game) popBag() int {
	if len(g.bag) == 0 {
		g.bag = []int{0, 1, 2, 3, 4, 5, 6}
		g.rng.Shuffle(len(g.bag), func(i, j int) { g.bag[i], g.bag[j] = g.bag[j], g.bag[i] })
	}
	v := g.bag[0]
	g.bag = g.bag[1:]
	return v
}

func (g *Game) spawn() {
	g.cur = activePiece{
		kind: g.nextKind,
		rot:  0,
		x:    3,
		y:    0,
	}
	g.nextKind = g.popBag()
	if g.collides(g.cur) {
		g.gameOver = true
	}
}

func (g *Game) pieceCells(ap activePiece) []point {
	src := pieceShapes[ap.kind][ap.rot]
	dst := make([]point, len(src))
	for i, p := range src {
		dst[i] = point{ap.x + p.x, ap.y + p.y}
	}
	return dst
}

func (g *Game) collides(ap activePiece) bool {
	for _, p := range g.pieceCells(ap) {
		if p.x < 0 || p.x >= boardW || p.y >= boardH {
			return true
		}
		if p.y >= 0 && g.board[p.y][p.x] != 0 {
			return true
		}
	}
	return false
}

func (g *Game) lockPiece() {
	for _, p := range g.pieceCells(g.cur) {
		if p.y < 0 {
			g.gameOver = true
			return
		}
		g.board[p.y][p.x] = g.cur.kind + 1
	}
	g.clearLines()
	g.spawn()
}

func (g *Game) clearLines() {
	newRows := make([][boardW]int, 0, boardH)
	cleared := 0
	for y := 0; y < boardH; y++ {
		full := true
		for x := 0; x < boardW; x++ {
			if g.board[y][x] == 0 {
				full = false
				break
			}
		}
		if full {
			cleared++
		} else {
			newRows = append(newRows, g.board[y])
		}
	}
	for len(newRows) < boardH {
		newRows = append([][boardW]int{{}}, newRows...)
	}
	for y := 0; y < boardH; y++ {
		g.board[y] = newRows[y]
	}
	if cleared > 0 {
		g.lines += cleared
		g.level = g.lines / 10
		scoreTable := []int{0, 40, 100, 300, 1200}
		if cleared >= 0 && cleared <= 4 {
			g.score += scoreTable[cleared] * (g.level + 1)
		}
	}
}

func (g *Game) tryMove(dx, dy int) bool {
	next := g.cur
	next.x += dx
	next.y += dy
	if !g.collides(next) {
		g.cur = next
		return true
	}
	return false
}

func (g *Game) tryRotate(dir int) bool {
	next := g.cur
	next.rot = (next.rot + dir + 4) % 4
	// simple wall kicks
	for _, ox := range []int{0, -1, 1, -2, 2} {
		test := next
		test.x += ox
		if !g.collides(test) {
			g.cur = test
			return true
		}
	}
	return false
}

func (g *Game) hardDrop() {
	for g.tryMove(0, 1) {
	}
	g.lockPiece()
	g.dropFrameCounter = 0
}

func gravityFrames(level int) int {
	// Faster as level increases; min 2 frames
	f := 30 - level*2
	if f < 2 {
		f = 2
	}
	return f
}

func (g *Game) Update() error {
	if g.gameOver {
		// Any key or touch to restart
		if inpututil.IsKeyJustPressed(ebiten.KeySpace) ||
			inpututil.IsKeyJustPressed(ebiten.KeyEnter) ||
			len(inpututil.AppendJustPressedTouchIDs(nil)) > 0 {
			g.Reset()
		}
		return nil
	}

	// Keyboard inputs
	if inpututil.IsKeyJustPressed(ebiten.KeyLeft) || inpututil.IsKeyJustPressed(ebiten.KeyA) {
		g.tryMove(-1, 0)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyRight) || inpututil.IsKeyJustPressed(ebiten.KeyD) {
		g.tryMove(1, 0)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyZ) {
		g.tryRotate(-1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyX) || inpututil.IsKeyJustPressed(ebiten.KeyUp) || inpututil.IsKeyJustPressed(ebiten.KeyW) {
		g.tryRotate(1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		g.hardDrop()
	}

	softDrop := ebiten.IsKeyPressed(ebiten.KeyDown) || ebiten.IsKeyPressed(ebiten.KeyS)

	// Touch inputs for mobile: simple 4-button layout at bottom
	if runtime.GOOS == "ios" || runtime.GOOS == "android" {
		w, h := ebiten.WindowSize()
		if w == 0 || h == 0 {
			w, h = logicalW, logicalH
		}
		ctrlH := 160
		btnY := h - ctrlH
		btnW := w / 4

		justIDs := inpututil.AppendJustPressedTouchIDs(nil)
		downIDs := ebiten.AppendTouchIDs(nil)

		justPressIn := func(ix int) bool {
			for _, id := range justIDs {
				x, y := ebiten.TouchPosition(id)
				if y >= btnY && x >= ix*btnW && x < (ix+1)*btnW {
					return true
				}
			}
			return false
		}
		pressIn := func(ix int) bool {
			for _, id := range downIDs {
				x, y := ebiten.TouchPosition(id)
				if y >= btnY && x >= ix*btnW && x < (ix+1)*btnW {
					return true
				}
			}
			return false
		}

		// Buttons: [0]=Left [1]=Right [2]=Rotate [3]=Drop (hard)
		if justPressIn(0) {
			g.tryMove(-1, 0)
		}
		if justPressIn(1) {
			g.tryMove(1, 0)
		}
		if justPressIn(2) {
			g.tryRotate(1)
		}
		if justPressIn(3) {
			g.hardDrop()
		}
		// Soft drop when any touch is held in the left half of the bottom area
		if pressIn(0) || pressIn(1) {
			softDrop = true
		}
	}

	// Gravity and soft drop
	g.dropFrameCounter++
	if softDrop {
		// faster drop when holding down
		if !g.tryMove(0, 1) {
			g.lockPiece()
		}
		g.dropFrameCounter = 0
	} else if g.dropFrameCounter >= gravityFrames(g.level) {
		if !g.tryMove(0, 1) {
			g.lockPiece()
		}
		g.dropFrameCounter = 0
	}

	return nil
}

func (g *Game) ghostPieceY() int {
	ghost := g.cur
	for !g.collides(activePiece{kind: ghost.kind, rot: ghost.rot, x: ghost.x, y: ghost.y + 1}) {
		ghost.y++
	}
	return ghost.y
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(bgColor)

	// Layout
	w, h := screen.Size()
	rightPanel := 150.0
	margin := 16.0
	ctrlH := 0.0
	if runtime.GOOS == "ios" || runtime.GOOS == "android" {
		ctrlH = 160.0
	}
	playWidth := float32(w) - float32(rightPanel) - float32(margin*3)
	playHeight := float32(h) - float32(margin*2) - float32(ctrlH)
	tile := minF(playWidth/boardW, playHeight/boardH)
	boardPxW := tile * boardW
	boardPxH := tile * boardH
	originX := float32(margin)
	originY := float32(margin)

	// Grid background
	vector.DrawFilledRect(screen, originX-2, originY-2, boardPxW+4, boardPxH+4, gridColor, false)

	// Board cells
	for y := 0; y < boardH; y++ {
		for x := 0; x < boardW; x++ {
			if g.board[y][x] != 0 {
				pc := pieceColors[g.board[y][x]-1]
				drawCell(screen, originX, originY, tile, x, y, pc)
			} else {
				// subtle grid
				gc := color.RGBA{30, 30, 44, 255}
				drawCell(screen, originX, originY, tile, x, y, gc)
			}
		}
	}

	// Current piece
	for _, p := range pieceShapes[g.cur.kind][g.cur.rot] {
		x := g.cur.x + p.x
		y := g.cur.y + p.y
		if y >= 0 && y < boardH && x >= 0 && x < boardW {
			pc := pieceColors[g.cur.kind]
			drawCell(screen, originX, originY, tile, x, y, pc)
		}
	}

	// Right panel info
	panelX := originX + boardPxW + float32(margin)
	text.Draw(screen, "Next", basicfont.Face7x13, int(panelX), int(originY+14), color.White)
	drawNext(screen, panelX, originY+20, tile, g.nextKind)

	text.Draw(screen, fmt.Sprintf("Score: %d", g.score), basicfont.Face7x13, int(panelX), int(originY+120), color.White)
	text.Draw(screen, fmt.Sprintf("Lines: %d", g.lines), basicfont.Face7x13, int(panelX), int(originY+140), color.White)
	text.Draw(screen, fmt.Sprintf("Level: %d", g.level), basicfont.Face7x13, int(panelX), int(originY+160), color.White)

	if !(runtime.GOOS == "ios" || runtime.GOOS == "android") {
		text.Draw(screen, "Controls:", basicfont.Face7x13, int(panelX), int(originY+190), color.White)
		text.Draw(screen, "←/→ Move", basicfont.Face7x13, int(panelX), int(originY+206), color.White)
		text.Draw(screen, "↓ Soft Drop", basicfont.Face7x13, int(panelX), int(originY+222), color.White)
		text.Draw(screen, "Z/X or ↑ Rotate", basicfont.Face7x13, int(panelX), int(originY+238), color.White)
		text.Draw(screen, "Space Hard Drop", basicfont.Face7x13, int(panelX), int(originY+254), color.White)
	}

	// Touch buttons
	if runtime.GOOS == "ios" || runtime.GOOS == "android" {
		drawTouchControls(screen)
	}

	// Game over overlay
	if g.gameOver {
		overlay := color.RGBA{0, 0, 0, 160}
		vector.DrawFilledRect(screen, 0, 0, float32(w), float32(h), overlay, false)
		msg := "Game Over"
		text.Draw(screen, msg, basicfont.Face7x13, w/2-len(msg)*3, h/2-10, color.White)
		hint := "Tap or Space/Enter to restart"
		text.Draw(screen, hint, basicfont.Face7x13, w/2-len(hint)*3, h/2+8, color.White)
	}
}

func drawCell(screen *ebiten.Image, originX, originY, tile float32, x, y int, c color.RGBA) {
	px := originX + float32(x)*tile
	py := originY + float32(y)*tile
	vector.DrawFilledRect(screen, px+1, py+1, tile-2, tile-2, c, false)
}

func drawNext(screen *ebiten.Image, px, py, tile float32, kind int) {
	scale := tile * 0.7
	offX := px + 8
	offY := py + 8
	c := pieceColors[kind]
	for _, p := range pieceShapes[kind][0] {
		x := offX + float32(p.x)*scale
		y := offY + float32(p.y)*scale
		vector.DrawFilledRect(screen, x+1, y+1, scale-2, scale-2, c, false)
	}
}

func drawTouchControls(screen *ebiten.Image) {
	w, h := screen.Size()
	ctrlH := float32(160)
	btnW := float32(w) / 4
	y := float32(h) - ctrlH
	bg := color.RGBA{255, 255, 255, 20}
	lblColor := color.RGBA{255, 255, 255, 200}
	for i := 0; i < 4; i++ {
		vector.DrawFilledRect(screen, float32(i)*btnW, y, btnW-2, ctrlH-2, bg, false)
	}
	labels := []string{"Left", "Right", "Rotate", "Drop"}
	for i, s := range labels {
		tx := int(float32(i)*btnW + btnW/2 - float32(len(s))*3)
		ty := int(y + ctrlH/2)
		text.Draw(screen, s, basicfont.Face7x13, tx, ty, lblColor)
	}
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func (g *Game) Layout(ow, oh int) (int, int) {
	return logicalW, logicalH
}

func main() {
	ebiten.SetWindowSize(logicalW, logicalH)
	ebiten.SetWindowTitle("Tetris Clone (Go + Ebitengine)")
	game := NewGame()
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
