;;;; /lcars: a Star-Trek-style bridge console built from gtf panels.
;;;;
;;;;   /load examples/lcars.tf
;;;;   /lcars             sensor console: radar, contact list, forward viewscreen
;;;;   /lcars eng         engineering console: power, warp core, damage control
;;;;   /lcars alert red   (or: /lcars alert off)
;;;;   /lock N            lock the sensors on contact N      /pwr SYS LEVEL  set power
;;;;   /lcars stop        back to the plain tf screen
;;;;
;;;; Needs about 120x30 or more.  The contacts here are simulated so it runs with no game.
;;;; In a real game you would fill the same variables from GMCP/MSDP instead, e.g.
;;;;     /def -hGMCP -t'Ship.Contacts' ~lc_feed = /set lc_b1=$[gmcp_get("Ship.Contacts.0.brg")]
;;;; or bind panels straight to gmcp:... references; the layout does not care where data comes from.
;;;; The game input line is the normal tf input window at the bottom, so commands typed there
;;;; (to the MUSH, or /lock, /pwr) work while the console is up.

/loaded gtf/lcars.tf

/set lc_gen=0
/set lc_pi=3.14159265
/set lc_sw=0
/set lc_alert=off
/set lc_lock=0
/set lc_sd=47634.4
/set lc_hull=100
/set lc_core=62
/set lc_trend=50

;; ---- the six simulated contacts: name, type (F friend, H hostile, N neutral, U unknown),
;;      bearing (degrees, 0 = dead ahead), range (percent of sensor range), drift (deg/tick)
/set lc_n1=ENDEAVOUR
/set lc_t1=F
/set lc_b1=40
/set lc_r1=35
/set lc_d1=2
/set lc_n2=KLINGON BoP
/set lc_t2=H
/set lc_b2=330
/set lc_r2=70
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
/set lc_b5=10
/set lc_r5=20
/set lc_d5=-4
/set lc_n6=ROMULAN
/set lc_t6=H
/set lc_b6=290
/set lc_r6=90
/set lc_d6=1

;; ---- engineering power levels (percent)
/set lc_pw_warp=80
/set lc_pw_impulse=40
/set lc_pw_shields=70
/set lc_pw_weapons=55
/set lc_pw_sensors=90
/set lc_pw_life=100

;; helpers for the contact tables: get/put "<var><n>" through /eval
/def -i lc_get = /eval /set lc_v=%%{%1%2}
/def -i lc_put = /eval /set %1%2=%3

;; ---------------------------------------------------------------- simulation
/def -i lc_move = \
    /let i=1%; \
    /while (i <= 6) \
        /lc_get lc_b %i%; /let b=%lc_v%; \
        /lc_get lc_d %i%; /let d=%lc_v%; \
        /lc_put lc_b %i $[mod(b + d + 360, 360)]%; \
        /let i=$[i + 1]%; \
    /done%; \
    /set lc_sw=$[mod(lc_sw + 28, 360)]%; \
    /set lc_core=$[lc_core + rand(-3, 3)]%; \
    /set lc_core=$[lc_core > 95 ? 95 : (lc_core < 40 ? 40 : lc_core)]%; \
    /set lc_trend=$[rand(20, 80)]%; \
    /set lc_sd=$[lc_sd + 0.1]

;; draw the radar (13 rows x 25 columns, a circle of radius 6 rows) into lc_row0..lc_row12
/def -i lc_radar = \
    /let s=$[lc_sw * lc_pi / 180]%; \
    /let sn=$[sin(s)]%; /let cs=$[cos(s)]%; \
    /let r=0%; \
    /while (r <= 12) \
        /let row=%; \
        /let up=$[6 - r]%; \
        /let c=0%; \
        /while (c <= 24) \
            /let dx=$[(c - 12) / 2.0]%; \
            /let d=$[sqrt(dx * dx + up * up)]%; \
            /let dot=$[dx * sn + up * cs]%; \
            /let crs=$[dx * cs - up * sn]%; \
            /let ch=$[d > 6.4 ? " " : (d > 5.5 ? "." : ((dot > 0 & abs(crs) < 0.5) ? "/" : (d < 0.4 ? "+" : (abs(d - 3) < 0.35 ? "'" : " "))))]%; \
            /let row=$[strcat(row, ch)]%; \
            /let c=$[c + 1]%; \
        /done%; \
        /lc_put lc_row %r %row%; \
        /let r=$[r + 1]%; \
    /done%; \
    /let i=1%; \
    /while (i <= 6) \
        /lc_get lc_b %i%; /let b=%lc_v%; \
        /lc_get lc_r %i%; /let rg=%lc_v%; \
        /let a=$[b * lc_pi / 180]%; \
        /let cc=$[12 + trunc(12 * rg / 100.0 * sin(a) + 0.5)]%; \
        /let cr=$[6 - trunc(6 * rg / 100.0 * cos(a) + 0.5)]%; \
        /lc_get lc_row %cr%; /let old=%lc_v%; \
        /let new=$[strcat(substr(old, 0, cc), i, substr(old, cc + 1))]%; \
        /lc_put lc_row %cr %new%; \
        /let i=$[i + 1]%; \
    /done

/set lc_s0=  .      *        .     .       *     .       .   *      .  
/set lc_s1=     .        .      *        .          .    *       .     
/set lc_s2=*      .   .       .      *       .  .        .       *    .
/set lc_s3=   .       *     .          .       .      *       .    .   
/set lc_s4=.    .         *       .        *        .      .      *   

;; the viewscreen: 5 rows x 56 columns of stars; contacts in the forward 120 degrees
;; appear as [n] at their bearing, and lc_vlist names them
/def -i lc_view = \
    /set lc_v0=$[substr(strcat(lc_s0, lc_s0), 0, 64)]%; /set lc_v1=$[substr(strcat(lc_s1, lc_s1), 0, 64)]%; /set lc_v2=$[substr(strcat(lc_s2, lc_s2), 0, 64)]%; /set lc_v3=$[substr(strcat(lc_s3, lc_s3), 0, 64)]%; /set lc_v4=$[substr(strcat(lc_s4, lc_s4), 0, 64)]%; \
    /set lc_vlist=%; \
    /let i=1%; \
    /while (i <= 6) \
        /lc_get lc_b %i%; /let b=%lc_v%; \
        /let rel=$[b > 180 ? b - 360 : b]%; \
        /if (abs(rel) <= 60) \
            /lc_get lc_r %i%; /let rg=%lc_v%; \
            /let col=$[32 + trunc(rel * 30 / 60.0)]%; \
            /let vr=$[mod(i, 3) + 1]%; \
            /eval /set lc_old=%%{lc_v%{vr}}%; \
            /set lc_old=$[strcat(substr(lc_old, 0, col), "[", i, "]", substr(lc_old, col + 3))]%; \
            /eval /set lc_v%{vr}=%{lc_old}%; \
            /lc_get lc_n %i%; \
            /let nm=$[substr(lc_v, 0, 9)]%; \
            /set lc_vlist=%{lc_vlist} [%{i}] %{nm} %{rel}°  %; \
        /endif%; \
        /let i=$[i + 1]%; \
    /done

/def -i lc_tick = \
    /let g=%1%; \
    /if (lc_gen == g) \
        /lc_move%; /lc_radar%; /lc_view%; \
        /repeat -1 1 /lc_tick %g%; \
    /endif

;; ---------------------------------------------------------------- the panels
;; a row of the contact table: lock marker, id, name, type colour, bearing, range
/def -i lc_crow = \
    /set lc_rowtxt={if lc_lock == %1}>{else} {/if}%1 {lc_n%1\|pad:-11} {if lc_t%1 == "H"}{color red}H{/color}{elif lc_t%1 == "F"}{color green}F{/color}{elif lc_t%1 == "N"}{color #99ccff}N{/color}{else}{color yellow}{lc_t%1}{/color}{/if} {lc_b%1\|%%03d} {lc_r%1\|%%3d}

/def -i lc_head = \
    /panel -dock top -h 1 head = {color black on #ff9900} LCARS {/color}{color black on #cc99cc} %1 {/color}{color black on #9999ff} STARDATE {lc_sd\|%%.1f} {/color}{if lc_alert == "red"}{color white on #cc0000}{bold} RED ALERT {/bold}{/color}{else}{color black on #99ccff} CONDITION GREEN {/color}{/if}{color black on #ffcc99} HULL {lc_hull}%% {/color}

/def -i lc_sensors = \
    /layout new lcars-sensors%; \
    /lc_head SENSORS%; \
    /let t={color #ff9900}{bold}RANGE 40000 KM   SWEEP {lc_sw\|%%3d}{/bold}{/color}\\n%; \
    /let r=0%; \
    /while (r <= 12) \
        /let t=%{t}{color #33ff66}{lc_row%{r}}{/color}\\n%; \
        /let r=$[r + 1]%; \
    /done%; \
    /let t=%{t}{color #ff9900}{hr}{/color}\\n{color #ffcc66}  ID NAME        T BRG RNG{/color}%; \
    /let i=1%; \
    /while (i <= 6) \
        /lc_crow %i%; \
        /let t=%{t}\\n%{lc_rowtxt}%; \
        /let i=$[i + 1]%; \
    /done%; \
    /panel -dock left -w 31 -border rounded -fg #ff9900 -title TACTICAL_SENSORS radar = %{t}%; \
    /panel -dock right -w 26 -border rounded -fg #cc99cc -title SHIP_STATUS stat = {color #ffcc99}HULL{/color} {bar "lc_hull" 100 width=14 color=auto}\\n{color #ffcc99}SHLD{/color} {bar "lc_pw_shields" 100 width=14 color=#9999ff}\\n{color #ffcc99}WARP{/color} {bar "lc_pw_warp" 100 width=14 color=#ff9900}\\n{color #ffcc99}SENS{/color} {bar "lc_pw_sensors" 100 width=14 color=#33ff66}\\n{hr}\\n{if lc_lock > 0}{color white on #cc6666} TARGET LOCKED: {lc_lock} {/color}{else}{color black on #ffcc66} NO LOCK {/color}{/if}\\n{dim}/lock N to lock{/dim}%; \
    /panel -dock top -h 9 -border double -fg #9999ff -title FORWARD_VIEWSCREEN view = {color #ccddff}{lc_v0}\\n{lc_v1}\\n{lc_v2}\\n{lc_v3}\\n{lc_v4}{/color}\\n{color #ffcc66}IN VIEW:{/color}{lc_vlist}

/def -i lc_eng = \
    /layout new lcars-eng%; \
    /lc_head ENGINEERING%; \
    /panel -dock left -w 38 -border rounded -fg #ff9900 -title POWER_DISTRIBUTION power = {color #ffcc99}WARP    {/color}{bar "lc_pw_warp" 100 width=20 color=auto} {lc_pw_warp\|%%3d}\\n{color #ffcc99}IMPULSE {/color}{bar "lc_pw_impulse" 100 width=20 color=auto} {lc_pw_impulse\|%%3d}\\n{color #ffcc99}SHIELDS {/color}{bar "lc_pw_shields" 100 width=20 color=#9999ff} {lc_pw_shields\|%%3d}\\n{color #ffcc99}WEAPONS {/color}{bar "lc_pw_weapons" 100 width=20 color=#cc6666} {lc_pw_weapons\|%%3d}\\n{color #ffcc99}SENSORS {/color}{bar "lc_pw_sensors" 100 width=20 color=#33ff66} {lc_pw_sensors\|%%3d}\\n{color #ffcc99}LIFE    {/color}{bar "lc_pw_life" 100 width=20 color=#99ccff} {lc_pw_life\|%%3d}\\n{hr}\\n{dim}/pwr SYSTEM LEVEL{/dim}\\n{dim}warp impulse shields{/dim}\\n{dim}weapons sensors life{/dim}%; \
    /panel -dock right -w 30 -border rounded -fg #cc99cc -title DAMAGE_CONTROL dmg = {if lc_pw_shields < 30}{color red}SHIELDS   FAILING{/color}{else}{color green}SHIELDS   NOMINAL{/color}{/if}\\n{if lc_pw_warp > 90}{color yellow}WARP CORE OVERLOAD{/color}{else}{color green}WARP CORE NOMINAL{/color}{/if}\\n{if lc_core > 85}{color red}CORE TEMP CRITICAL{/color}{elif lc_core > 70}{color yellow}CORE TEMP HIGH{/color}{else}{color green}CORE TEMP OK{/color}{/if}\\n{if lc_pw_life < 50}{color red}LIFE SUPPORT LOW{/color}{else}{color green}LIFE SUPPORT OK{/color}{/if}\\n{if lc_hull < 60}{color red}HULL BREACH DECK 4{/color}{else}{color green}HULL INTEGRITY OK{/color}{/if}\\n{hr}\\n{color #ffcc66}core load{/color} {spark lc_trend width=14}%; \
    /panel -dock top -h 6 -border double -fg #9999ff -title WARP_CORE core = {color #ffcc99}CORE TEMP{/color} {bar "lc_core" 100 width=40 color=auto} {lc_core}%%\\n{color #ffcc99}PLASMA   {/color} {bar "lc_pw_warp" 100 width=40 color=#ff9900} {lc_pw_warp}%%\\n{color #ffcc99}COOLANT  {/color} {bar "lc_pw_life" 100 width=40 color=#99ccff}

;; ---------------------------------------------------------------- commands
/def -i lcars_stop = \
    /set lc_gen=$[lc_gen + 1]%; \
    /layout off%; \
    /echo -aBCgreen >>> LCARS console off, normal tf screen restored.

/def -i lcars_run = \
    /set lc_gen=$[lc_gen + 1]%; \
    /lc_move%; /lc_radar%; /lc_view%; \
    /if ({1} =~ "eng") /lc_eng%; \
    /else /lc_sensors%; \
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
