;;;; gtf startup: what gtf does once your personal config (~/.tfrc) has been loaded.
;;;; (Written in the language on purpose: it is gtf's own library, not part of the TinyFugue
;;;; library files gtf bundles. To change or turn off a step, redefine or /undef the macro
;;;; in your ~/.tfrc, which has already run by the time gtf calls it.)
;;;;
;;;; gtf calls /gtf_startup once, after the personal config, unless it was started with -f.

/loaded gtf/startup.tf

;; Load the world definitions file (the WORLDFILE macro: ~/tiny.world by default) so worlds saved
;; with /saveworld are there without a /loadworld line in the config. It runs after the config, so
;; worlds the config defines come first and still win the "connect to the first world" rule.
;; Turn it off with `/undef gtf_startup' in ~/.tfrc.
/def -i gtf_startup = \
    /let _wf=$[worldfile()]%; \
    /if ( _wf !~ "" ) \
        /let _fd=$[tfopen(_wf, "r")]%; \
        /if ( _fd >= 0 ) \
            /test tfclose(_fd)%; \
            /loadworld%; \
        /endif%; \
    /endif
