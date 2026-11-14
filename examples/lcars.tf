;;;; /lcars: a TNG-style LCARS bridge console, drawn with gtf canvases and panels.
;;;;
;;;;   /load examples/lcars.tf
;;;;   /lcars             sensor console: radar dish, contacts, forward viewscreen
;;;;   /lcars eng         engineering console: warp core, power, damage control
;;;;   /lcars alert red   (or: /lcars alert off)
;;;;   /lock N            lock sensors on contact N        /pwr SYS LEVEL   set power
;;;;   /lcars stop        back to the plain tf screen
;;;;
;;;; Best at 120x36 or bigger, in a truecolor terminal.  The contacts are simulated so it
;;;; runs with no game; in a real game you fill the same variables (lc_ct, lc_b1, ...) from
;;;; GMCP/MSDP and everything else stays the same.  Your normal tf input line is the
;;;; "game input" panel at the bottom: type MUSH commands, /lock, /pwr there.
;;;;
;;;; The frame is drawn with /canvas (rounded elbows, pill buttons); the radar is the
;;;; {radar} widget; the viewscreen and warp core are canvases redrawn every second.

/loaded gtf/lcars.tf

/set lc_gen=0
/set lc_mode=sens
/set lc_alert=off
/set lc_lock=0
/set lc_sw=0
/set lc_ph=0
/set lc_sd=47634.4
/set lc_hull=100
/set lc_core=62
/set lc_trend=50
/set lc_ct=
/set lc_py=0

;; the six simulated contacts: name, type (F friend, H hostile, N neutral, U unknown),
;; bearing (degrees, 0 = dead ahead), range (percent of sensor range), drift (deg/tick)
/set lc_n1=ENDEAVOUR
/set lc_t1=F
/set lc_b1=22
/set lc_r1=35
/set lc_d1=2
/set lc_n2=BIRD-OF-PREY
/set lc_t2=H
/set lc_b2=335
/set lc_r2=62
/set lc_d2=-3
/set lc_n3=DS-7 STATION
/set lc_t3=N
/set lc_b3=100
/set lc_r3=55
/set lc_d3=0
/set lc_n4=UNKNOWN
/set lc_t4=U
/set lc_b4=200
/set lc_r4=85
/set lc_d4=5
/set lc_n5=SHUTTLE 12
/set lc_t5=F
/set lc_b5=8
/set lc_r5=18
/set lc_d5=-2
/set lc_n6=ROMULAN
/set lc_t6=H
/set lc_b6=290
/set lc_r6=90
/set lc_d6=1

/set lc_pw_warp=80
/set lc_pw_impulse=40
/set lc_pw_shields=70
/set lc_pw_weapons=55
/set lc_pw_sensors=90
/set lc_pw_life=100

;; helpers for the contact tables: read/write "<var><n>" through /eval
/def -i lc_get = /eval /set lc_v=%%{%1%2}
/def -i lc_put = /eval /set %1%2=%3

;; ---------------------------------------------------------------- simulation
/def -i lc_move = \
    /let i=1%; \
    /let ct=%; \
    /while (i <= 6) \
        /lc_get lc_b %i%; /let b=%lc_v%; \
        /lc_get lc_d %i%; /let d=%lc_v%; \
        /lc_put lc_b %i $[mod(b + d + 360, 360)]%; \
        /lc_get lc_b %i%; /let b=%lc_v%; \
        /lc_get lc_r %i%; /let rg=%lc_v%; \
        /lc_get lc_t %i%; /let ty=%lc_v%; \
        /let ct=%{ct}%{i}:%{b}:%{rg}:%{ty};%; \
        /let i=$[i + 1]%; \
    /done%; \
    /set lc_ct=%{ct}%; \
    /set lc_sw=$[mod(lc_sw + 24, 360)]%; \
    /set lc_ph=$[lc_ph + 0.5]%; \
    /set lc_core=$[lc_core + rand(-3, 3)]%; \
    /set lc_core=$[lc_core > 95 ? 95 : (lc_core < 40 ? 40 : lc_core)]%; \
    /set lc_trend=$[rand(20, 80)]%; \
    /set lc_sd=$[lc_sd + 0.1]

;; ---------------------------------------------------------------- the frame
;; a navigation pill on the left column: /lc_pill LABEL HEIGHT COLOR
/def -i lc_pill = \
    /canvas rrect nav 0 %{lc_py} 16 %2 $[{2} / 2] %3 corners=tr,br%; \
    /canvas text nav 1 $[trunc((lc_py + {2} / 2 - 1) / 2)] #000000 bold=1 %1%; \
    /set lc_py=$[lc_py + {2} + 1]

;; the static LCARS frame: header with elbow, left column, footer with elbow
/def -i lc_frame = \
    /let w=$[term_width()]%; \
    /let h=$[term_height()]%; \
    /let nh=$[h - 9]%; \
    /let r=$[w - 6]%; \
    /let bm=$[w - 56]%; \
    /canvas new hdr %{w} 4%; \
    /canvas rrect hdr 0 0 16 8 8 #ff9900 corners=tl%; \
    /canvas fill hdr 16 4 4 4 #ff9900%; \
    /canvas disc hdr 20 8 4 #000000%; \
    /canvas fill hdr 16 0 30 4 #ff9900%; \
    /canvas fill hdr 47 0 20 4 #ffcc99%; \
    /canvas fill hdr 68 0 %{bm} 4 #cc99cc%; \
    /canvas fill hdr $[68 + bm + 1] 0 20 4 #9999ff%; \
    /canvas rrect hdr %{r} 0 6 4 2 #ff9900 corners=tr,br%; \
    /canvas text hdr 18 0 #000000 bold=1 47-1701%; \
    /canvas text hdr 49 0 #000000 bold=1 SENSORS%; \
    /canvas text hdr 70 0 #000000 bold=1 TACTICAL ARRAY%; \
    /canvas text hdr 18 2 #ff9900 bold=1 U.S.S. ENDEAVOUR   NCC-1701-F%; \
    /canvas text hdr $[w - 30] 2 #99ccff DEEP SPACE FRONTIER%; \
    /canvas new ftr %{w} 3%; \
    /canvas rrect ftr 0 0 16 6 6 #cc99cc corners=bl%; \
    /canvas fill ftr 16 2 40 4 #cc99cc%; \
    /canvas fill ftr 57 2 20 4 #ff9900%; \
    /canvas fill ftr 78 2 %{bm} 4 #9999ff%; \
    /canvas rrect ftr %{r} 2 6 4 2 #cc99cc corners=tr,br%; \
    /canvas text ftr 18 2 #000000 bold=1 GAME INPUT%; \
    /canvas text ftr 59 2 #000000 bold=1 /LOCK  /PWR%; \
    /canvas new nav 16 %{nh}%; \
    /set lc_py=0%; \
    /lc_pill SENSORS 6 #ff9900%; \
    /lc_pill RADAR 6 #ffcc99%; \
    /lc_pill TACTICAL 6 #cc99cc%; \
    /lc_pill NAVIGATE 6 #9999ff%; \
    /lc_pill ENGINEER 6 #ffcc66%; \
    /lc_pill COMMS 6 #cc6666%; \
    /canvas fill nav 0 %{lc_py} 16 3 #99ccff%; \
    /canvas rrect nav 0 $[lc_py + 4] 16 $[nh * 2 - lc_py - 4] 8 #cc99cc corners=bl

;; ---------------------------------------------------------------- the viewscreen
/def -i lc_ship = \
    /lc_get lc_b %1%; /let b=%lc_v%; \
    /lc_get lc_r %1%; /let rg=%lc_v%; \
    /lc_get lc_t %1%; /let ty=%lc_v%; \
    /lc_get lc_n %1%; /let nm=$[substr(lc_v, 0, 10)]%; \
    /let rel=$[b > 180 ? b - 360 : b]%; \
    /if (abs(rel) <= 62) \
        /let x=$[lc_vw / 2 + rel * (lc_vw / 2 - 8) / 62.0]%; \
        /let y=$[9 + mod({1} * 7, 12)]%; \
        /let s=$[6 - trunc(rg / 25)]%; \
        /if (ty =~ "F") /let c=#4da6ff%; /elseif (ty =~ "H") /let c=#ff4040%; /elseif (ty =~ "N") /let c=#ffd24d%; /else /let c=#e0e0e0%; /endif%; \
        /if (ty =~ "F") \
            /canvas poly view #2a5fb0 $[x - s * 0.25] $[y + s * 0.4] $[x + s * 0.25] $[y + s * 0.4] $[x + s * 0.35] $[y + s * 2.2] $[x - s * 0.35] $[y + s * 2.2]%; \
            /canvas line view $[x - s * 1.3] $[y + s * 1.0] $[x - s * 1.3] $[y + s * 2.8] #7ab8ff t=1.5%; \
            /canvas line view $[x + s * 1.3] $[y + s * 1.0] $[x + s * 1.3] $[y + s * 2.8] #7ab8ff t=1.5%; \
            /canvas glow view $[x - s * 1.3] $[y + s * 2.8] $[s * 1.2] #5ad1ff a=0.9%; \
            /canvas glow view $[x + s * 1.3] $[y + s * 2.8] $[s * 1.2] #5ad1ff a=0.9%; \
            /canvas ellipse view %{x} %{y} $[s * 1.5] $[s * 0.75] #c8defa%; \
            /canvas ellipse view %{x} $[y - s * 0.15] $[s * 1.0] $[s * 0.45] #eef6ff%; \
            /canvas disc view %{x} $[y - s * 0.1] $[s * 0.28] #7fd0ff%; \
        /elseif (ty =~ "H") \
            /canvas poly view #b02020 %{x} $[y - s * 0.6] $[x - s * 2.2] $[y + s * 1.4] $[x - s * 0.5] $[y + s * 0.5]%; \
            /canvas poly view #b02020 %{x} $[y - s * 0.6] $[x + s * 2.2] $[y + s * 1.4] $[x + s * 0.5] $[y + s * 0.5]%; \
            /canvas disc view %{x} %{y} $[s * 0.7] #ff6a4a%; \
            /canvas glow view %{x} %{y} $[s * 1.8] #40ff40 a=0.5%; \
        /elseif (ty =~ "N") \
            /canvas circle view %{x} %{y} $[s * 1.7] #ffd24d t=1.2%; \
            /canvas disc view %{x} %{y} $[s * 0.6] #ffefb0%; \
            /canvas line view $[x - s * 1.7] %{y} $[x + s * 1.7] %{y} #ffd24d%; \
            /canvas glow view %{x} %{y} $[s * 2.4] #ffd24d a=0.35%; \
        /else \
            /canvas glow view %{x} %{y} $[s * 2] #ffffff a=0.5%; \
            /canvas disc view %{x} %{y} $[s * 0.5] #f0f0f0%; \
        /endif%; \
        /canvas text view $[trunc(x) - 4] $[trunc((y + s * 3) / 2) + 1] %{c} %{nm}%; \
        /if (lc_lock == {1}) \
            /let k=$[s * 2.6 + 2]%; \
            /canvas line view $[x - k] $[y - k] $[x - k + 4] $[y - k] #ff9900 t=1.2%; \
            /canvas line view $[x - k] $[y - k] $[x - k] $[y - k + 4] #ff9900 t=1.2%; \
            /canvas line view $[x + k] $[y - k] $[x + k - 4] $[y - k] #ff9900 t=1.2%; \
            /canvas line view $[x + k] $[y - k] $[x + k] $[y - k + 4] #ff9900 t=1.2%; \
            /canvas line view $[x - k] $[y + k] $[x - k + 4] $[y + k] #ff9900 t=1.2%; \
            /canvas line view $[x - k] $[y + k] $[x - k] $[y + k - 4] #ff9900 t=1.2%; \
            /canvas line view $[x + k] $[y + k] $[x + k - 4] $[y + k] #ff9900 t=1.2%; \
            /canvas line view $[x + k] $[y + k] $[x + k] $[y + k - 4] #ff9900 t=1.2%; \
        /endif%; \
    /endif

/def -i lc_viewdraw = \
    /canvas clear view%; \
    /canvas stars view 0 0 %{lc_vw} 28 90 7 t=%{lc_ph}%; \
    /canvas disc view 12 34 23 #071433%; \
    /canvas arc view 12 34 0 22 300 80 #123470 a=0.9%; \
    /canvas arc view 12 34 0 22 330 40 #1f55a8 a=0.6%; \
    /canvas arc view 12 34 21 24 310 70 #8fc4ff a=0.7%; \
    /canvas arc view 12 34 23 25 320 50 #cfe4ff a=0.3%; \
    /canvas circle view $[lc_vw / 2] 14 7 #ff9900 a=0.45%; \
    /canvas line view $[lc_vw / 2 - 11] 14 $[lc_vw / 2 - 4] 14 #ff9900 a=0.45%; \
    /canvas line view $[lc_vw / 2 + 4] 14 $[lc_vw / 2 + 11] 14 #ff9900 a=0.45%; \
    /canvas line view $[lc_vw / 2] 3 $[lc_vw / 2] 9 #ff9900 a=0.45%; \
    /canvas line view $[lc_vw / 2] 19 $[lc_vw / 2] 25 #ff9900 a=0.45%; \
    /let i=1%; \
    /while (i <= 6) \
        /lc_ship %i%; \
        /let i=$[i + 1]%; \
    /done%; \
    /canvas text view 1 0 #ff9900 FORWARD VIEWSCREEN%; \
    /canvas text view $[lc_vw - 18] 0 #99ccff HEADING 000 MARK 000

;; ---------------------------------------------------------------- the warp core
/def -i lc_engdraw = \
    /canvas clear view%; \
    /let cx=$[trunc(lc_vw / 2)]%; \
    /canvas rrect view $[cx - 7] 1 14 26 5 #101c3c%; \
    /canvas rrect view $[cx - 6] 2 12 24 4 #16285a%; \
    /let k=0%; \
    /while (k < 9) \
        /let yy=$[2 + mod(k * 3 + trunc(lc_ph * 4), 24)]%; \
        /canvas fill view $[cx - 5] %{yy} 10 1 #4a7aff a=0.55%; \
        /let k=$[k + 1]%; \
    /done%; \
    /canvas glow view %{cx} 14 $[10 + 4 * sin(lc_ph * 2)] #6fb0ff a=0.9%; \
    /canvas rrect view $[cx - 3] 4 6 20 3 #cfe6ff a=0.8%; \
    /canvas rrect view $[cx - 22] 5 8 18 3 #ff9900%; \
    /canvas rrect view $[cx - 22] 5 8 $[trunc(18 * lc_pw_warp / 100)] 3 #ffd070%; \
    /canvas rrect view $[cx + 14] 5 8 18 3 #cc99cc%; \
    /canvas rrect view $[cx + 14] 5 8 $[trunc(18 * lc_pw_shields / 100)] 3 #e6c8e6%; \
    /canvas line view $[cx - 14] 14 $[cx - 7] 14 #ff9900 t=2%; \
    /canvas line view $[cx + 7] 14 $[cx + 14] 14 #cc99cc t=2%; \
    /canvas text view 1 0 #ff9900 WARP CORE ASSEMBLY%; \
    /canvas text view $[cx - 22] 12 #ffcc99 WARP%; \
    /canvas text view $[cx + 14] 12 #cc99cc SHLD

;; ---------------------------------------------------------------- the panels
;; one row of the contact list
/def -i lc_crow = \
    /set lc_rowtxt={if lc_lock == %1}{color #ff9900}>{/color}{else} {/if}%1 {lc_n%1\|pad:-11} {if lc_t%1 == "H"}{color #ff4040}H{/color}{elif lc_t%1 == "F"}{color #4da6ff}F{/color}{elif lc_t%1 == "N"}{color #ffd24d}N{/color}{else}{color #e0e0e0}{lc_t%1}{/color}{/if} {lc_b%1\|%%03d} {lc_r%1\|%%3d}

/def -i lc_common = \
    /layout new lcars-%1%; \
    /lc_frame%; \
    /set lc_vw=$[term_width() - 46]%; \
    /canvas new view %{lc_vw} 14%; \
    /panel -dock top -h 4 -border none hdr = {canvas hdr}%; \
    /panel -dock bottom -h 3 -border none ftr = {canvas ftr}%; \
    /panel -dock left -w 16 -border none nav = {canvas nav}

/def -i lc_sensors = \
    /lc_common sens%; \
    /let t={color #ff9900}{bold}STARDATE {lc_sd\|%%.1f}{/bold}{/color}{if lc_alert == "red"}{color white on #cc0000}{bold} RED ALERT {/bold}{/color}{/if}\\n%; \
    /let t=%{t}{radar contacts=lc_ct sweep=lc_sw width=29 height=15 trail=110 bg=#02080a}\\n%; \
    /let t=%{t}{color #ffcc66} ID NAME        T BRG RNG{/color}%; \
    /let i=1%; \
    /while (i <= 6) \
        /lc_crow %i%; \
        /let t=%{t}\\n%{lc_rowtxt}%; \
        /let i=$[i + 1]%; \
    /done%; \
    /let t=%{t}\\n{color #99ccff}HULL{/color} {bar "lc_hull" 100 width=22 gradient=#ff4040,#33ff66}\\n{color #99ccff}SHLD{/color} {bar "lc_pw_shields" 100 width=22 gradient=#3a78ff,#99ccff}%; \
    /panel -dock right -w 31 -border none info = %{t}%; \
    /panel -dock top -h 14 -border none view = {canvas view}

/def -i lc_eng = \
    /lc_common eng%; \
    /canvas text hdr 49 0 #000000 bold=1 ENGINEERING%; \
    /let t={color #ff9900}{bold}POWER DISTRIBUTION{/bold}{/color}{if lc_alert == "red"}{color white on #cc0000}{bold} RED ALERT {/bold}{/color}{/if}\\n%; \
    /let t=%{t}{color #ffcc99}WARP    {/color}{bar "lc_pw_warp" 100 width=17 gradient=#ff9900,#ffee99} {lc_pw_warp\|%%3d}\\n%; \
    /let t=%{t}{color #ffcc99}IMPULSE {/color}{bar "lc_pw_impulse" 100 width=17 gradient=#ff9900,#ffee99} {lc_pw_impulse\|%%3d}\\n%; \
    /let t=%{t}{color #ffcc99}SHIELDS {/color}{bar "lc_pw_shields" 100 width=17 gradient=#3a78ff,#99ccff} {lc_pw_shields\|%%3d}\\n%; \
    /let t=%{t}{color #ffcc99}WEAPONS {/color}{bar "lc_pw_weapons" 100 width=17 gradient=#cc3030,#ff9090} {lc_pw_weapons\|%%3d}\\n%; \
    /let t=%{t}{color #ffcc99}SENSORS {/color}{bar "lc_pw_sensors" 100 width=17 gradient=#22aa55,#99ffbb} {lc_pw_sensors\|%%3d}\\n%; \
    /let t=%{t}{color #ffcc99}LIFE    {/color}{bar "lc_pw_life" 100 width=17 gradient=#3a78ff,#99ccff} {lc_pw_life\|%%3d}\\n%; \
    /let t=%{t}{color #cc99cc}{hr width=29}{/color}\\n{color #ffcc66}CORE TEMP{/color} {bar "lc_core" 100 width=18 gradient=#33ff66,#ff4040} {lc_core}\\n%; \
    /let t=%{t}{scope lc_trend width=29 height=3 fill=on color=#66aaff}\\n%; \
    /let t=%{t}{if lc_pw_shields < 30}{color #ff4040}SHIELDS FAILING{/color}{else}{color #33ff66}SHIELDS NOMINAL{/color}{/if}\\n%; \
    /let t=%{t}{if lc_core > 85}{color #ff4040}CORE TEMP CRITICAL{/color}{elif lc_core > 70}{color #ffd24d}CORE TEMP HIGH{/color}{else}{color #33ff66}CORE TEMP OK{/color}{/if}\\n%; \
    /let t=%{t}{dim}/pwr SYSTEM LEVEL{/dim}%; \
    /panel -dock right -w 31 -border none info = %{t}%; \
    /panel -dock top -h 14 -border none view = {canvas view}

;; ---------------------------------------------------------------- the clock
/def -i lc_tick = \
    /let g=%1%; \
    /if (lc_gen == g) \
        /lc_move%; \
        /if (lc_mode =~ "eng") /lc_engdraw%; /else /lc_viewdraw%; /endif%; \
        /repeat -1 1 /lc_tick %g%; \
    /endif

;; ---------------------------------------------------------------- commands
/def -i lcars_stop = \
    /set lc_gen=$[lc_gen + 1]%; \
    /layout off%; \
    /echo -aBCgreen >>> LCARS console off, normal tf screen restored.

/def -i lcars_run = \
    /set lc_gen=$[lc_gen + 1]%; \
    /set lc_mode=%1%; \
    /lc_move%; \
    /if ({1} =~ "eng") /lc_eng%; /lc_engdraw%; \
    /else /lc_sensors%; /lc_viewdraw%; \
    /endif%; \
    /repeat -1 1 /lc_tick %{lc_gen}

/def -i lcars = \
    /if ({1} =~ "stop" | {1} =~ "off") /lcars_stop%; \
    /elseif ({1} =~ "alert") \
        /set lc_alert=$[{2} =~ "red" ? "red" : "off"]%; \
        /echo -aBCred >>> alert status: %lc_alert%; \
    /else /lcars_run %{*}%; \
    /endif

/def -i lock = \
    /if ({1} >= 1 & {1} <= 6) \
        /set lc_lock=%1%; \
        /echo -aBCcyan >>> sensors locked on contact %1%; \
    /else \
        /set lc_lock=0%; \
        /echo -aBCcyan >>> lock released%; \
    /endif

/def -i pwr = \
    /if ({2} < 0 | {2} > 100) \
        /echo -aBCred usage: /pwr warp|impulse|shields|weapons|sensors|life 0-100%; \
    /else \
        /eval /set lc_pw_%1=%2%; \
        /echo -aBCcyan >>> %1 power set to %2 percent%; \
    /endif

/echo -aBCgreen % LCARS console loaded: /lcars (sensors), /lcars eng, /lock N, /pwr SYS LEVEL, /lcars stop.
