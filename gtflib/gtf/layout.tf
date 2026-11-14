;;;; gtf layout helpers (gtf-only; not part of core TF).
;;;; The commands /panel and /layout are implemented by the client; this file
;;;; adds convenience macros on top of them.  See docs/LAYOUTS.md.

/loaded gtf/layout.tf

;; /sidebar [on|off|toggle]: show or hide the stock sidebar layout.
/def -i sidebar = \
    /if ({*} =~ "off" | {*} =~ "0") \
        /layout off%; \
    /else \
        /layout use sidebar%; \
    /endif

;; /panelhp name HPREF MAXREF: define a small one-line HP gauge panel docked on top.
;;   e.g. /panelhp hp gmcp:Char.Vitals.hp gmcp:Char.Vitals.maxhp
/def -i panelhp = \
    /panel -dock top -h 1 %1 = {gauge "%2" "%3" width=20 label=HP}

;; React to GMCP: remember the last Room.Info name for scripts.
/def -i -hGMCP ~gtf_gmcp_room = \
    /if ({1} =~ "Room.Info") /set gtf_room=$[gmcp_get("Room.Info.name")]%; /endif

;; The demos in gtf/examples/ load themselves the first time you use them: just type /lcars or /layoutdemo.
/def -i lcars = \
    /if (lcars_loaded !~ "1") \
        /set lcars_loaded=1%; \
        /eval /load -q %{GTFLIBDIR}/gtf/examples/lcars.tf%; \
        /lcars %{*}%; \
    /else \
        /echo -e %% lcars: cannot load %{GTFLIBDIR}/gtf/examples/lcars.tf%; \
    /endif

/def -i layoutdemo = \
    /if (layoutdemo_loaded !~ "1") \
        /set layoutdemo_loaded=1%; \
        /eval /load -q %{GTFLIBDIR}/gtf/examples/layout-demo.tf%; \
        /layoutdemo %{*}%; \
    /else \
        /echo -e %% layoutdemo: cannot load %{GTFLIBDIR}/gtf/examples/layout-demo.tf%; \
    /endif
