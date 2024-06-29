package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/deflix-tv/imdb2torrent"
	"github.com/gocolly/colly"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/odwrtw/tpb"
	"github.com/skratchdot/open-golang/open"
	"go.uber.org/zap"
)

var torrentInfo *torrent.Torrent
var selectedFile *SelectedFileInfo

type EpisodeInfo struct {
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title"`
	Quality   string `json:"quality"`
	InfoHash  string `json:"infoHash"`
	MagnetURL string `json:"magnetURL"`
}

type Result struct {
	Title     string `json:"title"`
	Quality   string `json:"quality"`
	InfoHash  string `json:"infoHash"`
	MagnetURL string `json:"magnetURL"`
}

type SelectedFileInfo struct {
	ID        int
	Name      string
	Sha       string
	Size      int64
	Extension string
	Category  string
}

func main() {
	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DataDir = "/tmp"
	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	searchClient := tpb.New(
		"https://apibay.org",
		"https://1337x.to",
		"https://yts.to",
	)

	r := mux.NewRouter()
	r.HandleFunc("/search/seeders", searchBySeedersHandler(searchClient)).Methods("GET")

	r.HandleFunc("/series/sources/{imdbID}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		imdbID := vars["imdbID"]
		seasonStr := r.FormValue("season")
		season, err := strconv.Atoi(seasonStr)
		if err != nil {
			responseWithError(w, "El parámetro 'season' debe ser un número entero", http.StatusBadRequest)
			return
		}

		if imdbID == "" {
			responseWithError(w, "El parámetro 'imdbID' es requerido", http.StatusBadRequest)
			return
		}

		imdbURL := "https://www.imdb.com/title/" + imdbID + "/"
		imdbInfo, err := scrapeIMDB(imdbURL)
		if err != nil {
			responseWithError(w, "Error al obtener información de IMDb", http.StatusInternalServerError)
			return
		}
		imdbTitle, ok := imdbInfo["title"].(string)
		//imdbYear, ok := imdbInfo["year"].(string)
		if !ok {
			responseWithError(w, "Error obtaining IMDb title", http.StatusInternalServerError)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		torrents, err := searchClient.Search(ctx, imdbTitle, &tpb.SearchOptions{
			Category: 208,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var filteredTorrents []*tpb.Torrent

		for _, t := range torrents {
			// Utiliza la función extractSeasonEpisode para obtener la información de temporada y episodio
			info, extractErr := extractSeasonEpisode(t.Name, season)
			if extractErr == nil && (season == 0 || info.Season == season) {
				// Agrega el torrent a la lista filtrada
				filteredTorrents = append(filteredTorrents, t)
			}
		}

		// Ordenar torrents filtrados por seeders (mayor a menor)
		sort.Slice(filteredTorrents, func(i, j int) bool {
			return filteredTorrents[i].Seeders > filteredTorrents[j].Seeders
		})

		// Write the response as JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(filteredTorrents)

	}).Methods("GET")

	r.HandleFunc("/movies/sources/{imdbID}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		imdbID := vars["imdbID"]
		if imdbID == "" {
			responseWithError(w, "El parámetro 'imdbID' es requerido", http.StatusBadRequest)
			return
		}
		imdbClient := imdb2torrent.NewYTSclient(imdb2torrent.DefaultYTSclientOpts, imdb2torrent.NewInMemoryCache(), zap.NewNop(), false)

		// Buscar torrents para la película con el IMDb ID proporcionado
		ytTorrents, err := imdbClient.FindMovie(context.Background(), imdbID)
		imdbURL := "https://www.imdb.com/title/" + imdbID + "/"
		imdbInfo, err := scrapeIMDB(imdbURL)
		if err != nil {
			responseWithError(w, "Error al obtener información de IMDb", http.StatusInternalServerError)
			return
		}

		// Obtain the IMDb title from the map
		imdbTitle, ok := imdbInfo["title"].(string)
		imdbYear, ok := imdbInfo["year"].(string)
		if !ok {
			responseWithError(w, "Error obtaining IMDb title", http.StatusInternalServerError)
			return
		}
		// Puedes crear un contexto para cancelar la búsqueda
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		torrents, err := searchClient.Search(ctx, imdbTitle, &tpb.SearchOptions{
			Category: 0,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Ordenar torrents por seeders (mayor a menor)
		sort.Slice(torrents, func(i, j int) bool {
			return torrents[i].Seeders > torrents[j].Seeders
		})

		// Create a common structure for both sources
		var results []Result

		// Map IMDb data to TorrentInfo structure for source 0 (ytTorrents)
		for _, yt := range ytTorrents {
			result := Result{
				Title:     yt.Title,
				Quality:   yt.Quality,
				InfoHash:  yt.InfoHash,
				MagnetURL: yt.MagnetURL,
			}
			results = append(results, result)
		}
		minSeeders := 3

		for _, t := range torrents {
			if t.Seeders >= minSeeders {
				if strings.Contains(t.Name, imdbYear) {
					quality := deduceQuality(t.Name)
					result := Result{
						Title:     imdbTitle,
						Quality:   quality,
						InfoHash:  t.InfoHash,
						MagnetURL: t.Magnet(),
					}
					results = append(results, result)
				}
			}
		}

		// Create a response structure to include both IMDb information and torrents
		mirrorsResponse := map[string]interface{}{
			"message": "Found",
			"mirrors": results,
		}

		// Write the response as JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mirrorsResponse)
	}).Methods("GET")

	r.HandleFunc("/{sha}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sha := vars["sha"]
		if sha == "" {
			responseWithError(w, "El parámetro 'sha' es requerido", http.StatusBadRequest)
			return
		}

		torrentURL := "magnet:?xt=urn:btih:" + sha

		torrentInfo, err = client.AddMagnet(torrentURL)
		if err != nil {
			responseWithError(w, "Error al agregar el torrent", http.StatusInternalServerError)
			return
		}

		<-torrentInfo.GotInfo()

		filesInfo := make([]SelectedFileInfo, 0)
		for idx, file := range torrentInfo.Files() {
			filesInfo = append(filesInfo, SelectedFileInfo{
				ID:        idx,
				Name:      file.Path(),
				Sha:       torrentInfo.InfoHash().HexString(),
				Size:      file.Length(),
				Extension: strings.TrimPrefix(filepath.Ext(file.Path()), "."),
				Category:  getCategory(strings.TrimPrefix(filepath.Ext(file.Path()), ".")),
			})
		}

		responseWithData(w, "OK", filesInfo)
	}).Methods("GET")

	r.HandleFunc("/{sha}/{id}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		sha := vars["sha"]
		id := vars["id"]

		if len(sha) != 40 {
			http.Error(w, "SHA inválido", http.StatusBadRequest)
			return
		}

		fileIndex, err := strconv.Atoi(id)
		if err != nil {
			http.Error(w, "Índice de archivo no válido", http.StatusBadRequest)
			return
		}

		torrentURL := "magnet:?xt=urn:btih:" + sha
		torrentInfo, err = client.AddMagnet(torrentURL)
		if err != nil {
			http.Error(w, "Error al agregar el torrent", http.StatusInternalServerError)
			return
		}

		<-torrentInfo.GotInfo()

		if fileIndex < 0 || fileIndex >= len(torrentInfo.Files()) {
			http.Error(w, "Índice de archivo no válido", http.StatusBadRequest)
			return
		}

		videoFile := torrentInfo.Files()[fileIndex]

		ext := strings.TrimPrefix(filepath.Ext(videoFile.Path()), ".")
		mimeType := getMimeType(ext)
		w.Header().Set("Content-Type", mimeType)

		reader := videoFile.NewReader()

		http.ServeContent(w, r, videoFile.Path(), time.Time{}, reader)
	}).Methods("GET")
	

	corsHandler := handlers.CORS(
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{"GET"}),
		handlers.AllowedHeaders([]string{"Content-Type"}),
	)

	err = http.ListenAndServe(":8080", corsHandler(r))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Abriendo el navegador web en http://localhost:8080")
	err = open.Run("http://localhost:8080")
	if err != nil {
		log.Fatal(err)
	}

	select {}
}

func extractSeasonEpisode(name string, requestedSeason int) (EpisodeInfo, error) {
	// Convertir el nombre a minúsculas para hacer la búsqueda sin distinción entre mayúsculas y minúsculas
	lowercaseName := strings.ToLower(name)

	// Buscar patrones que indiquen temporada y episodio en el nombre de la serie
	re := regexp.MustCompile(`(?:s|season)\D?(\d+)(?:e|episode)?(\d+)?`)
	matches := re.FindStringSubmatch(lowercaseName)

	if len(matches) >= 3 {
		season, _ := strconv.Atoi(matches[1])
		episode, _ := strconv.Atoi(matches[2])

		// Verificar si la temporada coincide con la solicitada
		if requestedSeason == 0 || season == requestedSeason {
			return EpisodeInfo{Season: season, Episode: episode}, nil
		}
	}

	// Si no se encuentra ninguna información, devolver un error
	return EpisodeInfo{}, fmt.Errorf("no se pudo extraer la información de temporada y episodio del nombre")
}

func scrapeIMDB(url string) (map[string]interface{}, error) {
	// Utilizando la biblioteca colly para el scraping
	c := colly.NewCollector()

	// Set headers to mimic a request from a browser in the USA
	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3")
		r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
	})

	var imdbInfo = make(map[string]interface{})

	// Buscando el título
	c.OnHTML("h1 span.hero__primary-text", func(e *colly.HTMLElement) {
		title := strings.TrimSpace(e.Text)
		imdbInfo["title"] = title
	})

	// Buscando el año de lanzamiento
	c.OnHTML("a.ipc-link--baseAlt[href*='/releaseinfo']", func(e *colly.HTMLElement) {
		// Obtener el texto dentro del enlace
		year := strings.TrimSpace(e.Text)
		imdbInfo["year"] = year
	})

	// Buscando información adicional como clasificación y duración
	c.OnHTML("ul.ipc-inline-list", func(e *colly.HTMLElement) {
		// Recorriendo los elementos de la lista
		e.ForEach("li", func(index int, li *colly.HTMLElement) {
			switch index {
			case 1:
				// Clasificación
				rating := strings.TrimSpace(li.Text)
				imdbInfo["rating"] = rating
			case 2:
				// Duración
				duration := strings.TrimSpace(li.Text)
				imdbInfo["duration"] = duration
			}
		})
	})

	// Visitar la URL
	err := c.Visit(url)
	if err != nil {
		return nil, err
	}

	return imdbInfo, nil
}
func searchBySeedersHandler(client *tpb.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Puedes crear un contexto para cancelar la búsqueda
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Extraer la consulta de búsqueda de los parámetros de la solicitud
		query := r.FormValue("q")

		// Extraer la categoría de los parámetros de la solicitud
		categoryParam := r.FormValue("category")

		var category tpb.TorrentCategory
		if categoryParam != "" {
			// Si se proporciona una categoría, utilizarla
			categoryInt, err := strconv.Atoi(categoryParam)
			if err != nil {
				http.Error(w, "Invalid category parameter", http.StatusBadRequest)
				return
			}
			category = tpb.TorrentCategory(categoryInt)
		} else {
			// Si no se proporciona la categoría, buscar en todas las categorías (incluyendo las no categorizadas)
			category = 0 // Puedes asignar un valor específico que indique "todas las categorías" según la lógica de tu aplicación
		}

		// Buscar en la categoría especificada o en todas las categorías
		torrents, err := client.Search(ctx, query, &tpb.SearchOptions{
			Category: category,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Ordenar torrents por seeders (mayor a menor)
		sort.Slice(torrents, func(i, j int) bool {
			return torrents[i].Seeders > torrents[j].Seeders
		})

		// Escribir la respuesta JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(torrents)
	}
}
func searchHandler(client *tpb.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Puedes crear un contexto para cancelar la búsqueda
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Extraer la consulta de búsqueda de los parámetros de la solicitud
		query := r.FormValue("q")

		// Extraer la categoría de los parámetros de la solicitud
		categoryParam := r.FormValue("category")

		var category tpb.TorrentCategory
		if categoryParam != "" {
			// Si se proporciona una categoría, utilizarla
			categoryInt, err := strconv.Atoi(categoryParam)
			if err != nil {
				http.Error(w, "Invalid category parameter", http.StatusBadRequest)
				return
			}
			category = tpb.TorrentCategory(categoryInt)
		} else {
			// Si no se proporciona la categoría, buscar en todas las categorías (incluyendo las no categorizadas)
			category = 0 // Puedes asignar un valor específico que indique "todas las categorías" según la lógica de tu aplicación
		}

		// Buscar en la categoría especificada o en todas las categorías
		torrents, err := client.Search(ctx, query, &tpb.SearchOptions{
			Category: category,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Ordenar torrents por seeders (mayor a menor)
		sort.Slice(torrents, func(i, j int) bool {
			return torrents[i].Seeders > torrents[j].Seeders
		})

		// Escribir la respuesta JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(torrents)
	}
}

// Utilidades
func getMimeType(extension string) string {
	switch extension {
	case "mp4":
		return "video/mp4"
	case "mkv":
		return "video/x-matroska"
	case "avi":
		return "video/x-msvideo"
	case "mov":
		return "video/quicktime"
	case "webm":
		return "video/webm"
	case "flv":
		return "video/x-flv"
	case "wmv":
		return "video/x-ms-wmv"
	case "mp3":
		return "audio/mpeg"
	case "ogg":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "srt":
		return "application/x-subrip"
	case "ass":
		return "application/x-subtitle-ass"
	case "vtt":
		return "text/vtt"
	default:
		return "application/octet-stream"
	}
}

func getCategory(extension string) string {
	switch extension {
	case "mp4", "mkv", "avi", "mov", "webm", "flv", "wmv":
		return "Video"
	case "mp3", "ogg", "wav":
		return "Audio"
	case "jpg", "jpeg", "png", "gif", "bmp":
		return "Photo"
	case "srt", "ass", "vtt":
		return "Subtitles"
	case "nes", "snes", "unif", "unf", "smc", "fig", "sfc", "gd3", "gd7", "dx2", "bsx", "swc", "z64", "n64", "pce", "iso", "ngp", "ngc", "ws", "wsc", "col", "cv", "d64", "nds", "gba", "gb":
		return "Games"
	case "pdf", "epub", "mobi":
		return "Books"
	default:
		return "Otro"
	}
}

// Modelos de respuesta
func responseWithError(w http.ResponseWriter, message string, statusCode int) {
	response := map[string]interface{}{
		"message": message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// Función para deducir la calidad y el tipo del título
func deduceQuality(title string) string {
	// Lista de posibles calidades y tipos
	possibleQualities := []string{"720p", "1080p", "2160p", "480p", "360p", "HDRip", "WEB-DL", "BluRay"}

	// Itera sobre las posibles calidades y tipos y verifica si alguno está presente en el título
	for _, quality := range possibleQualities {
		if strings.Contains(strings.ToLower(title), strings.ToLower(quality)) {
			// Verifica si también hay un tipo asociado (como "web")
			typeIndex := strings.Index(strings.ToLower(title), strings.ToLower(quality))
			if typeIndex != -1 && typeIndex+len(quality)+2 < len(title) {
				typeStr := strings.TrimSpace(title[typeIndex+len(quality) : typeIndex+len(quality)+2])
				return fmt.Sprintf("%s (%s)", quality, typeStr)
			}

			// Si no hay un tipo, simplemente devuelve la calidad
			return quality
		}
	}

	// Si no se encuentra ninguna calidad específica, devolver una cadena vacía o un valor por defecto según sea necesario
	return "Unknown"
}

func responseWithData(w http.ResponseWriter, message string, data interface{}) {
	response := map[string]interface{}{
		"message": message,
		"data":    data,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
