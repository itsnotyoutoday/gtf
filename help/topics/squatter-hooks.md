---
title: Squatter hooks
topics: squatter-hooks
section: Remote access
order: 90
---
# Squatter hooks

The squatter fires these hooks. `%{*}` is `device-name role`; the name may contain spaces, so use `%{*}` or `%1 ...`.

| hook | when |
|---|---|
| `SQUAT_PAIR` | a device was just paired |
| `SQUAT_CONNECT` | a device connected |
| `SQUAT_DISCONNECT` | a device disconnected |

```
/def -hSQUAT_CONNECT = /echo -aB phone connected: %{*}
```

See [[Squatter]].
