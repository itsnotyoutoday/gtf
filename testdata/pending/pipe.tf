/def src = /test echo("one")%; /test echo("two")
/def two = /src %| /while (tfread(l) >= 0) /echo got:%l%; /done
/two
/def three = /test echo("a b") %| /test tfread(x)
/def four = /src %| /test tfread(x)%; /echo x=%{x}
/four
