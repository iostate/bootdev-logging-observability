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