package servermanager

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
)

type vec struct {
	x    float64
	z    float64
	relX float64
	relZ float64

	color color.RGBA
}

func (rh *ResultsHandler) renderCollisions(w http.ResponseWriter, r *http.Request) {
	var collisionsToLoad []int

	fileName := chi.URLParam(r, "fileName")

	if !smresults.ValidFileName(fileName + ".json") {
		http.NotFound(w, r)
		return
	}

	result, err := LoadResult(fileName + ".json")

	if os.IsNotExist(err) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("could not get result")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	collisions := r.URL.Query().Get("collisions")

	if collisions == "all" {
		for z := range result.Events {
			collisionsToLoad = append(collisionsToLoad, z)
		}
	} else {
		splitCollisions := strings.Split(collisions, ",")

		for i := range splitCollisions {

			collisionNum, err := strconv.Atoi(splitCollisions[i])

			if err != nil {
				continue
			}

			collisionsToLoad = append(collisionsToLoad, collisionNum)
		}
	}

	trackMapData, err := smcontent.LoadTrackMapData(ServerInstallPath, result.TrackName, result.TrackConfig)

	if err != nil {
		logrus.WithError(err).Error("could not load track map data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	trackMapImage, err := smcontent.LoadTrackMapImage(ServerInstallPath, result.TrackName, result.TrackConfig)

	if err != nil {
		logrus.WithError(err).Error("could not load track map image")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	var collisionVectors []vec

	for i, collision := range result.Events {

		var mainColor color.RGBA

		if collision.Type == "COLLISION_WITH_ENV" {
			mainColor = color.RGBA{R: 255, G: 125, B: 0, A: 0xff}
		} else {
			mainColor = color.RGBA{R: 255, G: 0, B: 0, A: 0xff}
		}

		for z := range collisionsToLoad {
			if collisionsToLoad[z] == i {
				collisionVectors = append(collisionVectors, vec{
					x:     (collision.WorldPosition.X + trackMapData.OffsetX) / trackMapData.ScaleFactor,
					z:     (collision.WorldPosition.Z + trackMapData.OffsetZ) / trackMapData.ScaleFactor,
					relX:  collision.RelPosition.X,
					relZ:  collision.RelPosition.Z,
					color: mainColor,
				})
			}
		}
	}

	img := image.NewRGBA(image.Rectangle{Min: image.Pt(0, 0), Max: image.Pt(trackMapImage.Bounds().Max.X, trackMapImage.Bounds().Max.Y)})
	radius := 4

	for _, collisionVector := range collisionVectors {
		if collisionVector.relX > 0 || collisionVector.relZ > 0 {
			// show the relative collision position
			draw.Draw(img, img.Bounds(), &circle{image.Pt(int(collisionVector.x+collisionVector.relX), int(collisionVector.z+collisionVector.relZ)), radius, color.RGBA{R: 0, G: 0, B: 255, A: 0xff}}, image.Pt(0, 0), draw.Over)
		}

		draw.Draw(img, img.Bounds(), &circle{image.Pt(int(collisionVector.x), int(collisionVector.z)), radius, collisionVector.color}, image.Pt(0, 0), draw.Over)
	}

	err = png.Encode(w, img)

	if err != nil {
		logrus.WithError(err).Error("could not encode image")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

type circle struct {
	p     image.Point
	r     int
	color color.RGBA
}

func (c *circle) ColorModel() color.Model {
	return color.AlphaModel
}

func (c *circle) Bounds() image.Rectangle {
	return image.Rect(c.p.X-c.r, c.p.Y-c.r, c.p.X+c.r, c.p.Y+c.r)
}

func (c *circle) At(x, y int) color.Color {
	xx, yy, rr := float64(x-c.p.X)+0.5, float64(y-c.p.Y)+0.5, float64(c.r)
	if xx*xx+yy*yy < rr*rr {
		return c.color
	}
	return color.RGBA{0, 0, 0, 0}
}
