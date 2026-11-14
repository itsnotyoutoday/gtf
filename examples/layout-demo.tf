;;;; gtf layout demo: a guided tour of /panel and /layout.
;;;;
;;;;   /load examples/layout-demo.tf
;;;;   /layoutdemo        run the whole tour (about 55 seconds, one scene every 3 seconds)
;;;;   /layoutdemo stop   stop early and put the normal tf screen back
;;;;   /layoutdemo N      jump straight to scene N (1-18)
;;;;
;;;; No MUD connection is needed.  The panels are driven by TF variables, which
;;;; update live; against a real game the same panels can read gmcp:... or
;;;; msdp:... instead.  The tour never writes any file.  See docs/LAYOUTS.md.

/set demo_gen=0
/set demo_scenes=18
/set demo_hp=100
/set demo_maxhp=100
/set demo_mp=60
/set demo_maxmp=60
/set demo_xp=1234
/set demo_name=aria the bold
/set demo_room=The Crossroads
/set demo_gold=1234567
/set demo_ratio=0.8731
/set demo_trend=10

;; caption shown in the output window for each scene
/def -i demo_say = /echo -aBCcyan >>> scene %1: %-1

;; one animation tick: hp sags, mp refills, trend jitters
/def -i demo_tick = \
    /if (demo_gen == {1}) \
        /set demo_hp=$[demo_hp <= 8 ? 100 : demo_hp - 7]%; \
        /set demo_mp=$[demo_mp >= 60 ? 0 : demo_mp + 6]%; \
        /set demo_trend=$[rand(5, 55)]%; \
        /set demo_xp=$[demo_xp + rand(1, 40)]%; \
        /repeat -1 1 /demo_tick %1%; \
    /endif

;; run scene N now (if this tour is still the current one)
/def -i demo_step = /if (demo_gen == {1}) /demo_scene %2%; /endif

/def -i demo_stop = \
    /set demo_gen=$[demo_gen + 1]%; \
    /layout off%; \
    /echo -aBCgreen >>> layout demo stopped, normal tf screen restored.

/def -i demo_run = \
    /demo_stop%; \
    /set demo_gen=$[demo_gen + 1]%; \
    /set demo_hp=100%; /set demo_mp=60%; \
    /echo -aBCgreen >>> layout demo starting, /layoutdemo stop to quit.%; \
    /repeat -1 1 /demo_tick %demo_gen%; \
    /let t=1%; \
    /let n=1%; \
    /while (n <= demo_scenes) \
        /repeat -%t 1 /demo_step %demo_gen %n%; \
        /let n=$[n + 1]%; \
        /let t=$[t + 3]%; \
    /done

;; ---------------------------------------------------------------- scenes
/def -i demo_scene = \
    /if ({1} == 1) /demo_s1%; \
    /elseif ({1} == 2) /demo_s2%; \
    /elseif ({1} == 3) /demo_s3%; \
    /elseif ({1} == 4) /demo_s4%; \
    /elseif ({1} == 5) /demo_s5%; \
    /elseif ({1} == 6) /demo_s6%; \
    /elseif ({1} == 7) /demo_s7%; \
    /elseif ({1} == 8) /demo_s8%; \
    /elseif ({1} == 9) /demo_s9%; \
    /elseif ({1} == 10) /demo_s10%; \
    /elseif ({1} == 11) /demo_s11%; \
    /elseif ({1} == 12) /demo_s12%; \
    /elseif ({1} == 13) /demo_s13%; \
    /elseif ({1} == 14) /demo_s14%; \
    /elseif ({1} == 15) /demo_s15%; \
    /elseif ({1} == 16) /demo_s16%; \
    /elseif ({1} == 17) /demo_s17%; \
    /elseif ({1} == 18) /demo_s18%; \
    /else /echo demo_scene: no scene {1} (1-%demo_scenes)%; \
    /endif

;; 1. A single top bar with styling tags
/def -i demo_s1 = \
    /demo_say 1 a one-line top panel with styles%; \
    /layout new demo%; \
    /panel -dock top -h 1 bar = {bold}bold{/bold} {dim}dim{/dim} {italic}italic{/italic} {underline}underline{/underline} {reverse}reverse{/reverse} {color red}red{/color} {color white on blue}white on blue{/color} {color #ff8800}orange{/color}

;; 2. Variables, expressions and filters in a bordered box on the left
/def -i demo_s2 = \
    /demo_say 2 variables, expressions and filters%; \
    /layout clear%; \
    /panel -dock left -w 30 -border rounded -title Values vals = name: {demo_name|title}\\nshout: {demo_name|upper}\\nshort: {demo_name|trunc:8}\\ngold:  {demo_gold|k}\\nratio: {demo_ratio|%%.2f}\\nhp%%:  {expr:demo_hp*100/demo_maxhp}\\nnone:  {var:no_such_var|default:(unset)}

;; 3. A live HP bar that changes colour
/def -i demo_s3 = \
    /demo_say 3 live bars: color=auto goes green, yellow, red as hp falls%; \
    /layout clear%; \
    /panel -dock top -h 2 bars = HP {bar "demo_hp" "demo_maxhp" width=30 color=auto} {demo_hp}/{demo_maxhp}\\nMP {bar "demo_mp" "demo_maxmp" width=30 color=blue} {demo_mp}/{demo_maxmp}

;; 4. Bar styles
/def -i demo_s4 = \
    /demo_say 4 bar styles: ascii, custom fill, custom colours%; \
    /layout clear%; \
    /panel -dock bottom -h 4 -border single -title Bar-styles styles = {bar "demo_hp" "demo_maxhp" width=24 color=auto chars=ascii} ascii\\n{bar "demo_mp" "demo_maxmp" width=24 color=#00cccc bg=#003333} truecolor\\n{bar "demo_hp" "demo_maxhp" width=24 fill=# empty=. color=yellow} fill and empty

;; 5. Gauges
/def -i demo_s5 = \
    /demo_say 5 gauges with labels, brackets and percentages%; \
    /layout clear%; \
    /panel -dock right -w 34 -border double -title Gauges gauges = {gauge "demo_hp" "demo_maxhp" width=12 label=HP}\\n{gauge "demo_mp" "demo_maxmp" width=12 label=MP pct=on}\\n{gauge "demo_hp" "demo_maxhp" width=12 label=HP brackets=off nums=off}

;; 6. Sparkline of a changing value
/def -i demo_s6 = \
    /demo_say 6 a sparkline of the recent history of a value%; \
    /layout clear%; \
    /panel -dock top -h 1 -fg green spark = trend: {spark demo_trend width=24} {demo_trend}

;; 7. Conditionals
/def -i demo_s7 = \
    /demo_say 7 conditionals: the text changes with hp%; \
    /layout clear%; \
    /panel -dock top -h 1 status = {if demo_hp < 30}{color red}{bold}DANGER{/bold}{/color}{elif demo_hp < 60}{color yellow}hurt{/color}{else}{color green}healthy{/color}{/if}  ({demo_hp} hp)  {if demo_mp >= 30 and demo_hp > 20}spell ready{else}resting{/if}

;; 8. Every border style
/def -i demo_s8 = \
    /demo_say 8 border styles: single rounded double ascii none%; \
    /layout clear%; \
    /panel -dock top -h 3 -border single -title single b1 = single%; \
    /panel -dock top -h 3 -border rounded -title rounded b2 = rounded%; \
    /panel -dock top -h 3 -border double -title double b3 = double%; \
    /panel -dock top -h 3 -border ascii -title ascii b4 = ascii%; \
    /panel -dock top -h 3 -border none -title none b5 = none, no box

;; 9. Sizes: fixed, percent and flex
/def -i demo_s9 = \
    /demo_say 9 sizes: 12 cells, 20%%, then flex * takes the rest%; \
    /layout clear%; \
    /panel -dock left -w 12 -border single -title 12 a = fixed 12\\n{hr}\\ncells%; \
    /panel -dock left -w 20%% -border single -title 20%% b = a fifth of the terminal\\n{hr}\\nresizes with it%; \
    /panel -dock left -w * -border single -title * c = flex *%;

;; 10. Rules and line breaks
/def -i demo_s10 = \
    /demo_say 10 rules and breaks across the panel width%; \
    /layout clear%; \
    /panel -dock right -w 32 -border rounded -title Rules r = top\\n{hr}\\nmiddle{br}after a break{br}{hr}\\nbottom

;; 11. Panel colours
/def -i demo_s11 = \
    /demo_say 11 panel default colours: names, #rrggbb and 256:N%; \
    /layout clear%; \
    /panel -dock top -h 1 -fg white -bg red c1 = named colours%; \
    /panel -dock top -h 1 -fg #101010 -bg #ffcc00 c2 = truecolor #rrggbb%; \
    /panel -dock top -h 1 -fg 256:231 -bg 256:25 c3 = 256-colour palette

;; 12. Multi-panel dashboard
/def -i demo_s12 = \
    /demo_say 12 a dashboard: several panels together, all live%; \
    /layout clear%; \
    /panel -dock top -h 1 -bg #202040 head = {bold}{demo_name|title}{/bold} {dim}in{/dim} {demo_room}%; \
    /panel -dock left -w 30 -border rounded -title Vitals vit = HP {bar "demo_hp" "demo_maxhp" width=14 color=auto} {demo_hp|%%3d}\\nMP {bar "demo_mp" "demo_maxmp" width=14 color=blue} {demo_mp|%%3d}\\nXP {demo_xp|k}\\n{spark demo_trend width=20}%; \
    /panel -dock bottom -h 1 foot = {dim}gold{/dim} {demo_gold|k}   {if demo_hp < 30}{color red}LOW HP{/color}{/if}

;; 13. Hide, show and toggle at run time
/def -i demo_s13 = \
    /demo_say 13 panels hiding and showing on command%; \
    /panel -hide vit%; \
    /repeat -1 1 /panel -show vit%; \
    /repeat -2 1 /panel -toggle foot

;; 14. Narrow terminals: -minw
/def -i demo_s14 = \
    /demo_say 14 -minw hides a panel when the terminal is too narrow%; \
    /panel -dock right -w 30 -minw 120 -border single -title Wide-only wide = shown at 120+ columns\\nresize the terminal to\\nsee it come and go

;; 15. Listing the panels
/def -i demo_s15 = \
    /demo_say 15 /panel -list shows what is defined%; \
    /panel -list

;; 16. A second layout and switching
/def -i demo_s16 = \
    /demo_say 16 two layouts: switch between them with /layout use%; \
    /layout new minimal%; \
    /panel -dock bottom -h 1 -fg cyan mini = minimal layout: {demo_name} / {demo_room}%; \
    /repeat -1 1 /layout use demo

;; 17. The layout list
/def -i demo_s17 = \
    /demo_say 17 /layout list and show%; \
    /layout list%; \
    /layout show

;; 18. Finish
/def -i demo_s18 = \
    /demo_say 18 done. Back to the plain tf screen.%; \
    /demo_stop%; \
    /echo -aBCgreen Try it yourself: /panel -dock top -h 1 hi = hello {demo_name}%; \
    /echo -aBCgreen /layoutdemo to run it again, docs/LAYOUTS.md for everything else.

/def -i layoutdemo = \
    /if ({*} =~ "stop") /demo_stop%; \
    /elseif ({*} =~ "") /demo_run%; \
    /else /demo_scene %{*}%; \
    /endif

/echo -aBCgreen % layout demo loaded: type /layoutdemo to start (/layoutdemo stop to quit).
