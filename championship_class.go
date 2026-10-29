package servermanager

// ChampionshipClassColors are sequentially selected to indicate different classes within a Championship
var ChampionshipClassColors = []string{
	"#9ec6f5",
	"#91d8af",
	"#dba8ed",
	"#e3a488",
	"#e3819a",
	"#908ba1",
	"#a2b5b9",
	"#a681b4",
	"#c1929d",
	"#999ecf",
}

func ChampionshipClassColor(i int) string {
	return ChampionshipClassColors[i%len(ChampionshipClassColors)]
}
