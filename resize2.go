// +build go1.7

package bimg

import (
	"bytes"
	"fmt"
	"math"

	"gitlab.p1staff.com/backend/magick"
)

const WEBP_MAX_Pixel_Dimensions = 16383

// Resize is used to transform a given image as byte buffer
// with the passed options.
func Resize2(bs []byte, o Options) ([]byte, error) {
	image, err := magick.DecodeData(bs)
	if err != nil {
		return nil, err
	}
	defer image.Dispose()

	fileExt := ImageTypes[o.Type]

	switch image.Format() {
	case "WEBP":
		o.Quality = 100
	case "GIF":
		if o.Type != GIF {
			return nil, fmt.Errorf("image format: %s mismatch with request: [%s]", image.Format(), fileExt)
		}
		return bs, nil
	}

	image = scaleImage(image, o)

	image = effectImage(image, o)

	if o.Crop {
		w, h := o.Width, o.Height
		// if smaller than request format, use original image to crop
		if isImageSmallerThanFormatSize(image, o) {
			w = int(math.Min(float64(image.Width()), float64(image.Height())))
			h = w * o.Height / o.Width
		}

		x := (image.Width() - w) / 2
		y := (image.Height() - h) / 2
		image, err = image.Crop(magick.Rect{X: x, Y: y, Width: uint(w), Height: uint(h)})
		if err != nil {
			return nil, err
		}
	}

	image, err = autoOrient(image)
	if err != nil {
		return nil, err
	}

	info := magick.NewInfo()
	info.SetQuality(uint64(o.Quality))

	//key1[=[value1]],key2[=[value2]]
	if fileExt == "webp" {
		err = info.AddDefinitions("webp:lossless=false,webp:image-hint=photo,webp:thread-level=1")
		if err != nil {
			fmt.Println(err)
			// slog.Debug("Webp: %+v", err)
		}
	}

	info.SetFormat(fileExt)
	buf := &bytes.Buffer{}

	// To remove any EXIF data we copy the contents to a new image object.
	if !image.Strip() {
		fmt.Println("Remove image EXIF failed!")
	}

	err = image.Encode(buf, info)
	if err != nil {
		return nil, err
	}

	if fileExt == "png" {
		buf2, err := optimizePng(buf.Bytes(), 8)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewBuffer(buf2)
	}

	return buf.Bytes(), nil
}

func scaleImage(image *magick.Image, o Options) *magick.Image {
	newWidth, newHeight := getDimensions(image, o)
	if newWidth == image.Width() && newHeight == image.Height() {
		return image
	}

	image, err := image.ResizeBlur(o.Width, o.Height, magick.FLanczos, 1.1)
	if err != nil {
		panic(err)
	}

	return image
}

func getDimensions(image *magick.Image, c Options) (int, int) {
	w, h := image.Width(), image.Height()
	if isImageSmallerThanFormatSize(image, c) && !c.Enlarge {
		return correctDimensions(image, w, h, c.Type)
	}

	resizeMethod := "auto"

	if c.Crop {
		resizeMethod = "crop"
	} else if c.Width != 0 && c.Height == 0 {
		resizeMethod = "landscape"
	} else if c.Width == 0 && c.Height != 0 {
		resizeMethod = "portrait"
	}

	switch resizeMethod {
	case "exact":
		w, h = c.Width, c.Height
	case "portrait":
		w, h = getSizeByFixedHeight(image, c.Height), c.Height
	case "landscape":
		w, h = c.Width, getSizeByFixedWidth(image, c.Width)
	case "auto":
		return getSizeByAuto(image, c.Width, c.Height)
	case "crop":
		w, h = c.Width, c.Height
		w, h = getOptimalCrop(image, w, h)
	}

	return correctDimensions(image, w, h, c.Type)
}

func getOptimalCrop(image *magick.Image, newWidth, newHeight int) (int, int) {
	heightRatio := float64(image.Height()) / float64(newHeight)
	widthRatio := float64(image.Width()) / float64(newWidth)

	var optimalRatio float64
	if heightRatio < widthRatio {
		optimalRatio = heightRatio
	} else {
		optimalRatio = widthRatio
	}

	optimalHeight := float64(image.Height()) / optimalRatio
	optimalWidth := float64(image.Width()) / optimalRatio
	return int(optimalWidth + 0.5), int(optimalHeight + 0.5)
}

func getSizeByAuto(image *magick.Image, newWidth, newHeight int) (int, int) {
	if isImageLandscape(image) {
		return newWidth, getSizeByFixedWidth(image, newWidth)
	}
	if isImagePortrait(image) {
		return getSizeByFixedHeight(image, newHeight), newHeight
	}
	// Image to be resized is a square
	if newHeight > newWidth {
		return newWidth, getSizeByFixedWidth(image, newWidth)
	}
	if newHeight < newWidth {
		return getSizeByFixedHeight(image, newHeight), newHeight
	}
	// Square being resized to a square
	return newWidth, newHeight

}

func isImageSmallerThanFormatSize(image *magick.Image, c Options) bool {

	isStrictlySmaller := image.Width() < c.Width && image.Height() < c.Height
	isSquareAndSmaller := isImageSquare(image) &&
		(image.Width() < c.Width || image.Height() < c.Height)
	isSmallerWidth := (c.Width == 0 && image.Height() < c.Height) ||
		(image.Height() == c.Height && image.Width() < c.Width)
	isSmallerHeight := (c.Height == 0 && image.Width() < c.Width) ||
		(image.Width() == c.Width && image.Height() < c.Height)

	return isStrictlySmaller || isSquareAndSmaller || isSmallerWidth || isSmallerHeight
}

func isImageSquare(image *magick.Image) bool {
	return image.Width() == image.Height()
}

func effectImage(image *magick.Image, o Options) *magick.Image {
	// Gaussian Blur Image
	var err error
	if o.GaussianBlur.Sigma > 0 {
		radius, sigma := o.GaussianBlur.MinAmpl, o.GaussianBlur.Sigma
		// Default sigma = 1
		if sigma == 0 {
			sigma = 1
		}

		image, err = image.GaussianBlur(radius, sigma)
		if err != nil {
			panic(err)
		}
	}

	return image
}

func correctDimensions(image *magick.Image, width, height int, fileExt ImageType) (int, int) {
	if fileExt != WEBP {
		return width, height
	}

	if width > WEBP_MAX_Pixel_Dimensions {
		width = WEBP_MAX_Pixel_Dimensions
		height = getSizeByFixedWidth(image, width)
	}

	if height > WEBP_MAX_Pixel_Dimensions {
		height = WEBP_MAX_Pixel_Dimensions
		width = getSizeByFixedHeight(image, height)
	}

	return width, height
}

func getSizeByFixedWidth(image *magick.Image, newWidth int) int {
	ratio := float64(image.Height()) / float64(image.Width())
	newHeight := float64(newWidth) * ratio
	return int(math.Floor(newHeight + 0.5))
}

func getSizeByFixedHeight(image *magick.Image, newHeight int) int {
	ratio := float64(image.Width()) / float64(image.Height())
	newWidth := float64(newHeight) * ratio
	return int(math.Floor(newWidth + 0.5))
}

func isImagePortrait(image *magick.Image) bool {
	return image.Height() > image.Width()
}

func isImageLandscape(image *magick.Image) bool {
	return image.Height() < image.Width()
}

func autoOrient(image *magick.Image) (*magick.Image, error) {
	m := &magick.AffineMatrix{}

	orientation := image.Property("EXIF:Orientation")

	// references
	// https://github.com/recurser/exif-orientation-examples
	// http://magnushoff.com/jpeg-orientation.html
	// http://stackoverflow.com/questions/5905868/how-to-rotate-jpeg-images-based-on-the-orientation-metadata
	// https://en.wikipedia.org/wiki/Affine_transformation
	// http://www.imagemagick.org/Usage/distorts/affine/

	switch orientation {
	case "2":
		m.Sx = -1
		m.Sy = 1
	case "3":
		m.Sx = -1
		m.Sy = -1
	case "4":
		m.Sx = 1
		m.Sy = -1
	case "5":
		m.Rx = 1
		m.Ry = 1
	case "6":
		m.Rx = 1
		m.Ry = -1
	case "7":
		m.Rx = -1
		m.Ry = -1
	case "8":
		m.Rx = -1
		m.Ry = 1
	default: //orientation=1 and no orientation. Use identity transform
		m.Sx = 1
		m.Sy = 1
	}

	image2, err := image.AffineTransform(m)
	if err != nil {
		return nil, err
	}
	image2.RemoveProperty("EXIF:Orientation")
	return image2, err
}
