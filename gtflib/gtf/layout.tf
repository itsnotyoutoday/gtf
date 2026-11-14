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
