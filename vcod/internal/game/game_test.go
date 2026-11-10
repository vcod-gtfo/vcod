package game

import (
	"reflect"
	"testing"

	"vcod/internal/config"
	"vcod/internal/ws"
)

func TestParseServerArgs(t *testing.T) {
	base := ServerOpts{Flavor: config.F15, Map: "mp_harbor", Gametype: "tdm", Port: 28960}
	o, err := ParseServerArgs(base, []string{"sd", "mp_carentan", "--port", "28970", "--cheats", "+set", "g_speed", "250"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Map != "mp_carentan" || o.Gametype != "sd" || o.Port != 28970 || !o.Cheats {
		t.Fatalf("got %+v", o)
	}
	if !reflect.DeepEqual(o.Extra, []string{"+set", "g_speed", "250"}) {
		t.Fatalf("extra = %v", o.Extra)
	}
	if o.Addr() != "127.0.0.1:28970" {
		t.Fatalf("addr = %s", o.Addr())
	}
	if _, err := ParseServerArgs(base, []string{"--bogus"}); err == nil {
		t.Fatal("accepted --bogus")
	}
	if _, err := ParseServerArgs(base, []string{"--port", "x"}); err == nil {
		t.Fatal("accepted a bad port")
	}
}

func TestParseClientArgs(t *testing.T) {
	base := ClientOpts{Flavor: config.F16x, Build: ws.LookupBuild("video"), Mode: -1}
	o, err := ParseClientArgs(base, []string{"10.0.0.2:28961", "--headless", "--mode", "4"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Connect != "10.0.0.2:28961" || o.Build.Name != "headless" || o.Mode != 4 {
		t.Fatalf("got %+v", o)
	}
}

func TestContainerNames(t *testing.T) {
	// run.sh names its containers after the flavor folder with the dots made underscores;
	// vcod must find the same names to stop a server.
	if Container(config.F15) != "cod_rea_server_1_5" || Container(config.F16x) != "cod_rea_server_1_6x" {
		t.Fatalf("%s %s", Container(config.F15), Container(config.F16x))
	}
}
