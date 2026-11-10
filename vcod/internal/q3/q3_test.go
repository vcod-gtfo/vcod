package q3

import "testing"

func TestParseStatus(t *testing.T) {
	pkt := "\xff\xff\xff\xffstatusResponse\n\\sv_hostname\\^1Red ^7Server\\mapname\\mp_harbor\\g_gametype\\tdm\n12 48 \"Pl ayer\"\n0 999 \"bot\"\n"
	st, err := ParseStatus([]byte(pkt))
	if err != nil {
		t.Fatal(err)
	}
	if st.Get("MapName") != "mp_harbor" || st.Get("g_gametype") != "tdm" {
		t.Fatalf("vars = %v", st.Vars)
	}
	if len(st.Players) != 2 || st.Players[0].Name != "Pl ayer" || st.Players[0].Score != 12 || st.Players[0].Ping != 48 {
		t.Fatalf("players = %+v", st.Players)
	}
	if st.Humans() != 1 {
		t.Fatalf("humans = %d", st.Humans())
	}
	if got := StripColors(st.Get("sv_hostname")); got != "Red Server" {
		t.Fatalf("StripColors = %q", got)
	}
}

func TestParseStatusRejectsOtherPackets(t *testing.T) {
	if _, err := ParseStatus([]byte("\xff\xff\xff\xffprint\nhello")); err == nil {
		t.Fatal("accepted a print packet")
	}
}

func TestStrip(t *testing.T) {
	body, ok := strip([]byte("\xff\xff\xff\xffprint\nmap: mp_harbor\n"), "print")
	if !ok || body != "map: mp_harbor\n" {
		t.Fatalf("strip = %q %v", body, ok)
	}
}

func TestChangesMap(t *testing.T) {
	for cmd, want := range map[string]bool{"map mp_harbor": true, "Map_Restart": true, "status": false, "g_gametype sd": false, "": false} {
		if ChangesMap(cmd) != want {
			t.Errorf("ChangesMap(%q) != %v", cmd, want)
		}
	}
}
