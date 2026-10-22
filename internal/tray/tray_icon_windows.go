//go:build windows

package tray

import (
	"bytes"
	"encoding/binary"
	"os/exec"

	"github.com/getlantern/systray"
)

// setIcon 设置 Windows 通知区域图标（ICO，32 位 BGRA）。
func setIcon() { systray.SetIcon(icoBytes()) }

// openBrowser 用系统默认浏览器打开链接。
func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// icoBytes 在内存里生成一个 16x16 的 ICO，避免往仓库里塞二进制资源。
func icoBytes() []byte {
	const size = 16
	const headerSize = 40
	xor := size * size * 4
	and := size * size / 8
	image := make([]byte, headerSize+xor+and)

	// BITMAPINFOHEADER：高度写两倍（XOR 位图 + AND 掩码）
	binary.LittleEndian.PutUint32(image[0:], headerSize)
	binary.LittleEndian.PutUint32(image[4:], size)
	binary.LittleEndian.PutUint32(image[8:], size*2)
	binary.LittleEndian.PutUint16(image[12:], 1)
	binary.LittleEndian.PutUint16(image[14:], 32)

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			offset := headerSize + (y*size+x)*4
			inside := x >= 2 && x < size-2 && y >= 2 && y < size-2
			bar := inside && (y == 6 || y == 9 || y == 12) && x >= 5 && x < size-4
			switch {
			case bar:
				image[offset+0], image[offset+1], image[offset+2], image[offset+3] = 20, 30, 25, 255
			case inside:
				image[offset+0], image[offset+1], image[offset+2], image[offset+3] = 118, 167, 59, 255
			default:
				image[offset+0], image[offset+1], image[offset+2], image[offset+3] = 0, 0, 0, 0
			}
		}
	}

	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	_ = binary.Write(&out, binary.LittleEndian, uint16(1)) // count
	out.WriteByte(size)                                    // width
	out.WriteByte(size)                                    // height
	out.WriteByte(0)                                       // palette
	out.WriteByte(0)                                       // reserved
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // planes
	_ = binary.Write(&out, binary.LittleEndian, uint16(32)) // bpp
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(image)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(22)) // offset
	out.Write(image)
	return out.Bytes()
}
