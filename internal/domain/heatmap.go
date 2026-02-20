package domain

type HeatmapCell struct {
	Date      string
	Count     int
	Intensity int
}

type HeatmapMatrix struct {
	Year  int
	Weeks [][]HeatmapCell
	Start string
	End   string
}
