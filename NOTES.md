# Info
Used to record important events that are not errors, i.e. "User X logged in",
"config.json loaded success", "server started on port 8080".

# Warn
Don't use. Usually should be denoted to INFO. 
Does the message represent any sort of potential bug? Error.
Does the message need to make its way to the user? Send a 400 and use Info.
Will the issue resolve itself? Don't even log it.

# Error
Include enough information to diagnose the problem.
- Error msg
- Stack trace 
- Context (user, permissions, external API)

Use pkg errors to attach stack trace. Use slog.Any with key "error" and error as value. Use a fn which checks for the presence of an error on an error slog key. Give that fn to ReplaceAttr method on the slog text/json handler.

# Slog-Specific 
GroupAttrs returns a {"error_group_name", errorAttrs...}
Use []slog.Attr slice to build attributes


# Notes 
- Some logs are errors, most errors are logs.