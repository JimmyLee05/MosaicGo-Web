package main

import (
	"net/http"
	"fmt"
	"html/template"
	"strconv"
	"image"
	"os"
	"image/draw"
	"bytes"
	"image/jpeg"
	"encoding/base64"
	"time"
	"sync"
)

var TILESDB map[string][3]float64

func main() {
	// 多路复用器
	mux := http.NewServeMux()
	/*  FileServer函数创建一个能为指定目录中的静态文件服务的处理器
 	 *  并将这个处理器传递给了多路复用器的Handle函数
 	 */
	files := http.FileServer(http.Dir("public"))
	/*  使用StripPrefix函数医促会请求URL中的指定前缀
 	 *  当服务器收到一个以/static/开头的URL请求时，上下两行代码
 	 *  会移除URL中的/static/字符串，然后在public目录中查找被请求的文件
 	 */
	mux.Handle("/static/", http.StripPrefix("/static/", files))
	// 参数一是URL路径，参数二是该URL的实际处理对象
	mux.HandleFunc("/", upload)
	mux.HandleFunc("/mosaic", mosaic)
	server := &http.Server{
		Addr: "0.0.0.0:8080",
		Handler: mux,
	}

	TILESDB = tilesDB()
	fmt.Println("Mosaic server started.")
	// 监听某个端口，启动服务，准备接收客户端的请求
	server.ListenAndServe()
}

/*  处理器函数func upload() 上传目标图片的页面
 *  函数的第一个参数是ResponseWriter接口，第二个参数是指向Request结构的指针
 *  upload函数会从Request结构中提取相关的信息，然后创建一个HTTP响应，最后通过
 *  ResponseWriter接口将响应返回给客户端
 */
func upload(w http.ResponseWriter, r *http.Request) {
	t, _ := template.ParseFiles("upload.html")
	t.Execute(w, nil)
}

func mosaic(w http.ResponseWriter, r *http.Request) {
	t0 := time.Now()

	/*  使用ParseMultipartForm方法对请求进行语法分析，
 	 *  然后访问相应的字段，就可以获得请求中包含的数据
 	 *  最大占用内存为10M
 	 */
	r.ParseMultipartForm(10485760)
	// 获取用户上传的目标图片
	file, _, _ := r.FormFile("image")
	defer file.Close()

	tileSize, _ := strconv.Atoi(r.FormValue("tile_size"))
	// 对用户上传的目标图片进行解码
	original, _, _ := image.Decode(file)
	// 获得上传图片的尺寸
	bounds := original.Bounds()
	// 复制瓷砖图数据库
	db := cloneTilesDB()

	/*  以扇形散开的方式切割图片以便单独进行处理
 	 *  cut函数以扇形散开(fan-out)模式将目标图片分割为多个子图片
 	 */
	c1 := cut(original, db, tileSize, bounds.Min.X, bounds.Min.Y, bounds.Max.X/2, bounds.Max.Y/2)
	c2 := cut(original, db, tileSize, bounds.Max.X/2, bounds.Min.Y, bounds.Max.X, bounds.Max.Y/2)
	c3 := cut(original, db, tileSize, bounds.Min.X, bounds.Max.Y/2, bounds.Max.X/2, bounds.Max.Y)
	c4 := cut(original, db, tileSize, bounds.Max.X/2, bounds.Max.Y/2, bounds.Max.X, bounds.Max.Y)

	// 以扇形聚拢方式将多个子图片合并成一张完成的图片
	c := combine(bounds, c1, c2, c3, c4)

	buffer1 := new(bytes.Buffer)
	jpeg.Encode(buffer1, original, nil)
	originalStr := base64.StdEncoding.EncodeToString(buffer1.Bytes())

	t1 := time.Now()
	images := map[string]string{
		"original": originalStr,
		"mosaic":   <-c,
		"duration": fmt.Sprintf("%v ", t1.Sub(t0)),
	}
	t, _ := template.ParseFiles("results.html")
	t.Execute(w, images)
}

/*  cut函数在内部以goroutine方式执行一个匿名函数，而这个匿名函数则会返回一个通道作为结果
 * 	函数的 db *DB参数。把指向 DB结构的引用传递给 DB结构，而不是仅仅传入一个映射
 */
func cut(original image.Image, db *DB, tileSize int, x1, y1, x2, y2 int) <-chan image.Image {
	// 这个通道将作为函数的执行结果返回给调用者
	channel := make(chan image.Image)
	// 每张瓷砖图片的起始点
	sourcePoint := image.Point{
		X:0,
		Y:0,
	}

	// 创建匿名的goroutine
	go func() {
		newImage := image.NewNRGBA(image.Rect(x1, y1, x2, y2))
		for y := y1; y < y2; y = y + tileSize {
			for x := x1; x < x2; x = x + tileSize {
				r, g, b, _ := original.At(x, y).RGBA()
				color := [3]float64{float64(r), float64(g), float64(b)}
				// 调用DB结构的nearest方法来获取最匹配的瓷砖图片
				nearest := db.getNearestTile(color)
				file, err := os.Open(nearest)
				if err == nil {
					img, _, err := image.Decode(file)
					if err == nil {
						// 将瓷砖图片缩放至指定大小
						t := resize(img, tileSize)
						tile := t.SubImage(t.Bounds())
						tileBounds := image.Rect(x, y, x + tileSize, y + tileSize)
						draw.Draw(newImage, tileBounds, tile, sourcePoint, draw.Src)
					} else {
						fmt.Println("error in decoding nearest color file", err, nearest)
					}
				} else {
					fmt.Println("error opening file when creating mosaic:", nearest)
				}
				file.Close()
			}
		}
		channel <- newImage.SubImage(newImage.Rect)
	}()

	return channel
}

// 将4张子图片重新合并成1张完成的马赛克图片
func combine(rec image.Rectangle, c1, c2, c3, c4 <-chan image.Image) <-chan string {

	// combine函数将返回一个通道作为执行结果
	channel := make(chan string)

	// 创建匿名函数
	go func() {
		// 使用等待组去同步各个子图片的复制操作
		var wg sync.WaitGroup
		newImage := image.NewNRGBA(rec)
		copyImg := func(dst draw.Image, r image.Rectangle, src image.Image, sp image.Point) {
			draw.Draw(dst, r, src, sp, draw.Src)
			// 每复制完一张子图片，就对计数器执行一次减一操作
			wg.Done()
		}
		// 将等待组的值设置为4
		wg.Add(4)
		var s1, s2, s3, s4 image.Image
		var ok1, ok2, ok3, ok4 bool
		// 在一个无限循环里面等待所有复制操作完成
		for {
			// 等待各个通道的返回值
			select {
			/*  多值格式来接收通道的返回值
 			 * 	第二个返回值会说明程序是否已成功接收到了各通道传送的子图片
 			 */	
			case s1, ok1 = <- c1:
				/*  goroutine里面的goroutine
 				 * 	接收完图片后，在goroutine里复制这些子图片
 			 	 */	
				go copyImg(newImage, s1.Bounds(), s1, image.Point{rec.Min.X, rec.Min.Y})
			case s2, ok2 = <- c2:
				go copyImg(newImage, s2.Bounds(), s2, image.Point{rec.Max.X / 2, rec.Min.Y})
			case s3, ok3 = <- c3:
				go copyImg(newImage, s3.Bounds(), s3, image.Point{rec.Min.X, rec.Max.Y / 2})
			case s4, ok4 = <- c4:
				go copyImg(newImage, s4.Bounds(), s4, image.Point{rec.Max.X / 2, rec.Max.Y / 2})
			}

			// 当所有通道被关闭后，跳出循环
			if ok1 && ok2 && ok3 && ok4 {
				break
			}
		}

		// 阻塞直到所有的子图片的复制操作都执行完毕为止
		wg.Wait()
		buffer2 := new(bytes.Buffer)
		jpeg.Encode(buffer2, newImage, nil)
		channel <- base64.StdEncoding.EncodeToString(buffer2.Bytes())
	}()

	return channel
}
