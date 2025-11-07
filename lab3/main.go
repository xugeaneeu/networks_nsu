package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	base_search_loc = "https://nominatim.openstreetmap.org/search"
)

type result[T any] struct {
	value T
	err   error
}

type location struct {
	id   string
	name string
	lat  float64
	lon  float64
}

type weather struct {
	temp        float64
	windSpeed   float64
	windDir     float64
	weatherCode int
	time        string
}

type place struct {
	pageId int
	title  string
	lat    float64
	lon    float64
}

type placeWithDescription struct {
	place       place
	description string
}

type combinedResult struct {
	loc     location
	weather weather
	places  []placeWithDescription
}

func main() {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)

	query := getLocationFromUser(reader)
	if query == "" {
		fmt.Println("Пустой запрос")
		return
	}

	fmt.Println("Поиск локаций")
	locCh := searchLocations(ctx, client, query)

	locRes := <-locCh
	if locRes.err != nil {
		fmt.Printf("Ошибка поиска локаций %v\n", locRes.err)
		return
	}
	if len(locRes.value) == 0 {
		fmt.Println("Ничего не найдено")
		return
	}
	fmt.Println("Найденные варианты")
	for i, loc := range locRes.value {
		fmt.Printf("[%d] %s\n", i, loc.name)
	}

	fmt.Println("Выберите индекс локации (номер в скобках):")
	fmt.Print("> ")
	idxRaw, _ := reader.ReadString('\n')
	idxRaw = strings.TrimSpace(idxRaw)
	idx, err := strconv.Atoi(idxRaw)
	if err != nil || idx < 0 || idx >= len(locRes.value) {
		fmt.Println("Неверный индекс, выход.")
		return
	}
	chosen := locRes.value[idx]
	fmt.Printf("Выбран: %s\n", chosen.name)

	fmt.Println("Получаем погоду и интересные места...")
	dataCh := fetchLocationData(ctx, client, chosen)

	result := <-dataCh
	if result.err != nil {
		fmt.Printf("Ошибка при получении данных: %v\n", result.err)
		return
	}

	cr := result.value
	fmt.Println("---- Результат ----")
	fmt.Printf("Локация: %s\n", cr.loc.name)
	fmt.Printf("Погода (время %s): Температура %.1f°C, Ветер %.1f m/s, Направление %.0f°, Код %d\n",
		cr.weather.time, cr.weather.temp, cr.weather.windSpeed, cr.weather.windDir, cr.weather.weatherCode)
	fmt.Printf("Найдено интересных мест: %d\n", len(cr.places))
	for i, pw := range cr.places {
		fmt.Printf("\n[%d] %s (lat %.6f lon %.6f)\n", i, pw.place.title, pw.place.lat, pw.place.lon)
		desc := strings.TrimSpace(pw.description)
		if desc == "" {
			fmt.Println("   Описание: (отсутствует)")
		} else {
			snippet := desc
			if len(snippet) > 800 {
				snippet = snippet[:800] + "…"
			}
			fmt.Printf("   Описание: %s\n", snippet)
		}
	}
	fmt.Println("-------------------")
}

func getLocationFromUser(reader *bufio.Reader) string {
	fmt.Println("Введите название локации для поиска (например, 'Цветной проезд' или 'Moscow'):")
	fmt.Print("> ")

	queryRaw, _ := reader.ReadString('\n')
	query := strings.TrimSpace(queryRaw)
	return query
}

func fetchJSON(ctx context.Context, c *http.Client, urlStr string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		return fmt.Errorf("http status %d: %s", resp.StatusCode, body)
	}

	dec := json.NewDecoder(resp.Body)
	return dec.Decode(out)
}

func searchLocations(ctx context.Context, c *http.Client, q string) <-chan result[[]location] {
	ch := make(chan result[[]location], 1)
	go func() {
		defer close(ch)
		v := url.Values{}
		v.Set("q", q)
		v.Set("format", "json")
		u := base_search_loc + "?" + v.Encode()

		var resp []struct {
			DisplayName string `json:"display_name"`
			Lat         string `json:"lat"`
			Lon         string `json:"lon"`
			PlaceID     int64  `json:"place_id"`
		}

		if err := fetchJSON(ctx, c, u, nil, &resp); err != nil {
			ch <- result[[]location]{err: err}
			return
		}

		locs := make([]location, 0, len(resp))
		for _, it := range resp {
			lat, _ := strconv.ParseFloat(it.Lat, 64)
			lon, _ := strconv.ParseFloat(it.Lon, 64)
			locs = append(locs, location{
				id:   strconv.FormatInt(it.PlaceID, 10),
				name: it.DisplayName,
				lat:  lat,
				lon:  lon,
			})
		}

		ch <- result[[]location]{value: locs}
	}()
	return ch
}

func fetchLocationData(parCtx context.Context, c *http.Client, loc location) <-chan result[combinedResult] {
	ch := make(chan result[combinedResult], 1)
	go func() {
		defer close(ch)

		ctx, cancel := context.WithCancel(parCtx)
		defer cancel()

		wch := fetchLocationWeather(ctx, c, loc)
		pch := fetchPlaces(ctx, c, loc)

		var weatherRes result[weather]
		var placesRes result[[]placeWithDescription]

		select {
		case weatherRes = <-wch:
		case <-ctx.Done():
			ch <- result[combinedResult]{err: ctx.Err()}
			return
		}
		if weatherRes.err != nil {
			cancel()
			ch <- result[combinedResult]{err: fmt.Errorf("weather error: %w", weatherRes.err)}
			return
		}

		select {
		case placesRes = <-pch:
		case <-ctx.Done():
			ch <- result[combinedResult]{err: ctx.Err()}
			return
		}
		if placesRes.err != nil {
			cancel()
			ch <- result[combinedResult]{err: fmt.Errorf("places error: %w", placesRes.err)}
			return
		}

		places := placesRes.value
		if len(places) == 0 {
			cr := combinedResult{
				loc:     loc,
				weather: weatherRes.value,
				places:  nil,
			}
			ch <- result[combinedResult]{value: cr}
			return
		}

		descCh := make(chan result[placeWithDescription], len(places))
		for _, p := range places {
			pl := p.place
			go func() {
				dch := fetchDescription(ctx, c, pl.pageId)
				select {
				case dr := <-dch:
					if dr.err != nil {
						descCh <- result[placeWithDescription]{err: fmt.Errorf("desc error for %s: %w", pl.title, dr.err)}
						return
					}
					descCh <- result[placeWithDescription]{value: placeWithDescription{
						place:       pl,
						description: dr.value,
					}}
				case <-ctx.Done():
					descCh <- result[placeWithDescription]{err: ctx.Err()}
				}
			}()
		}

		collected := make([]placeWithDescription, 0, len(places))
		for range places {
			select {
			case r := <-descCh:
				if r.err != nil {
					cancel()
					ch <- result[combinedResult]{err: r.err}
					return
				}
				collected = append(collected, r.value)
			case <-ctx.Done():
				ch <- result[combinedResult]{err: ctx.Err()}
				return
			}
		}

		cr := combinedResult{
			loc:     loc,
			weather: weatherRes.value,
			places:  collected,
		}
		ch <- result[combinedResult]{value: cr}
	}()

	return ch
}

func fetchLocationWeather(ctx context.Context, c *http.Client, loc location) <-chan result[weather] {
	return nil
}

func fetchPlaces(ctx context.Context, c *http.Client, loc location) <-chan result[[]placeWithDescription] {
	return nil
}

func fetchDescription(ctx context.Context, client *http.Client, pageid int) <-chan result[string] {
	return nil
}
