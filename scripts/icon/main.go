// 生成 App 图标（1024x1024 PNG）：蓝色圆角方块 + 白色 Wi-Fi 信号。
// 用法: go run ./scripts/icon out.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	const n = 1024
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	top := [3]float64{59, 130, 246}                                   // #3b82f6
	bot := [3]float64{29, 78, 216}                                    // #1d4ed8
	cx, cy := 512.0, 720.0                                            // Wi-Fi 圆心
	arcs := [][2]float64{{0, 58}, {150, 215}, {280, 345}, {410, 475}} // 圆点 + 三道弧（内径, 外径）

	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			// macOS 图标规范：内容区 824，四周留白 100，圆角 185
			a := coverage(roundedRectDist(fx, fy, 100, 100, 924, 924, 185))
			if a == 0 {
				continue
			}
			t := (fy - 100) / 824
			c := [3]float64{}
			for i := range c {
				c[i] = top[i] + (bot[i]-top[i])*t
			}
			// 白色信号：只画朝上 ±45° 的扇形
			dx, dy := fx-cx, fy-cy
			r := math.Hypot(dx, dy)
			ang := math.Atan2(-dy, dx) * 180 / math.Pi
			w := 0.0
			for i, ar := range arcs {
				d := math.Max(ar[0]-r, r-ar[1]) // 到圆环的距离
				if i > 0 {
					d = math.Max(d, (math.Abs(ang-90)-45)*r*math.Pi/180) // 扇形边界
				}
				w = math.Max(w, coverage(d))
			}
			for i := range c {
				c[i] = c[i]*(1-w) + 255*w
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(c[0]), uint8(c[1]), uint8(c[2]), uint8(a * 255)})
		}
	}
	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	png.Encode(f, img)
}

// coverage 把「到边界的有符号距离」变成 0~1 的覆盖率，边缘抗锯齿。
func coverage(d float64) float64 { return math.Max(0, math.Min(1, 0.5-d)) }

// roundedRectDist 点到圆角矩形边界的有符号距离（里面为负）。
func roundedRectDist(x, y, x0, y0, x1, y1, r float64) float64 {
	qx := math.Abs(x-(x0+x1)/2) - ((x1-x0)/2 - r)
	qy := math.Abs(y-(y0+y1)/2) - ((y1-y0)/2 - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}
