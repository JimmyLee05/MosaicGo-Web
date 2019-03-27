package main

import (
	"fmt"
	"io/ioutil"
	"image"
	"os"
	"math"
	"image/color"
	"sync"
)

type DB struct {
	mutex *sync.Mutex
	store map[string][3]float64
}

/*  将给定的图片缩放至指定大小
 *  图片缩放同时转换成NRGB类型的图片，作为返回值
 */
func resize(img image.Image, newWidth int) *image.NRGBA {
	bounds := img.Bounds()
	width := bounds.Dx()
	ratio := width / newWidth
	output := image.NewNRGBA(image.Rect(bounds.Min.X / ratio, bounds.Min.Y / ratio, bounds.Max.X / ratio, bounds.Max.Y / ratio))

   /*  NRGBA类型代表没有预乘alpha通道的32位RGB色彩，Red、Green、Blue、Alpha各8位。
	*  RGBA类型代表传统的预乘了alpha通道的32位RGB色彩，Red、Green、Blue、Alpha各8位。
	*  Alpha类型代表一个8位的alpha通道
	*  >>8 右移运算，先转换成二进制，再右移8位
	*/ 
	for y, j := bounds.Min.Y, bounds.Min.Y; y < bounds.Max.Y; y, j = y + ratio, j + 1 {
		for x, i := bounds.Min.X, bounds.Min.X; x < bounds.Max.X; x, i = x + ratio, i + 1 {
			r, g, b, a := img.At(x, y).RGBA()
			output.SetNRGBA(i, j, color.NRGBA{uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)})
		}
	}
	return output
}

// 计算给定图片的平均颜色
func averageColor(img image.Image) [3]float64 {
	bounds := img.Bounds()
	r, g, b := 0.0, 0.0, 0.0
	// 循环得到总共的红、绿、蓝颜色有多少
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, _ := img.At(x, y).RGBA()
			r, g, b = r + float64(r1), g + float64(g1), b + float64(b1)
		}
	}
	totalPixels := float64((bounds.Max.X - bounds.Min.X) * (bounds.Max.Y - bounds.Min.Y))
	fmt.Println(totalPixels)
	return [3]float64{r / totalPixels, g / totalPixels, b / totalPixels}
}

func cloneTilesDB() *DB {
	db := make(map[string][3]float64)
	for k, v := range TILESDB {
		db[k] = v
	}

	tiles := &DB{
		store: db,
		mutex: &sync.Mutex{},
	}
	return tiles
}

/*  通过扫描瓷砖图片所在目录来创建一个瓷砖图片数据库
 *  图片的名为键，图片的平均颜色为值
 */
func tilesDB() map[string][3]float64 {
	fmt.Println("Start populating tiles db ...")

	// make函数创建三元组的信道
	db := make(map[string][3]float64)
	// 读取瓷砖图片目录
	files, _ := ioutil.ReadDir("tiles")
	// 循环瓷砖图片文件夹，创建图片数据库
	for _, f := range files {
		name := "tiles/" + f.Name()
		file, err := os.Open(name)
		if err == nil {
			img, _, err := image.Decode(file)
			if err == nil {
				// 图片的名为键，图片的平均颜色为值
				db[name] = averageColor(img)
			} else {
				fmt.Println("error in populating tiles db:", err, name)
			}
		} else {
			fmt.Println("cannot open file", name, "when populating tiles db:", err)
		}
		_ = file.Close()
	}
	fmt.Println("Finished populating tiles db.")
	return db
}

// 寻找与目标图片相匹配的瓷砖图片
func (db *DB) getNearestTile(target [3]float64) string {
	var filename string
	smallest := 1e9

	// 数据库加互斥锁，避免两个goroutine找到同一个图片产生冲突
	db.mutex.Lock()
	// 数据库里的键值对，图片名是键，三元组为值
	for k, v := range db.store {
		distance := distance(target, v)
		if distance < smallest {
			filename = k
			smallest = distance
		}
	}
	// 2000多张的瓷砖库n样本不够多，不能满足高分辨率的图片
	//delete(tilesDB, filename)
	db.mutex.Unlock()
	return filename
}

// 计算两个三元组之间的欧几里得距离
func distance(color1 [3]float64, color2 [3]float64) float64 {
	return math.Sqrt(square(color1[0] - color2[0]) + square(color1[1] - color2[1]) + square(color1[2] - color2[2]))
}

// 计算给定数值的平方
func square(n float64) float64 {
	return n * n
}
