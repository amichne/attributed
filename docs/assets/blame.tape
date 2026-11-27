# Record the README's blame demo from the checkout root:
#
#   vhs docs/assets/blame.tape
#
# docs/assets/blame-setup.sh builds git-attributed from this checkout and
# prepares a repository where Ada wrote add() and Claude Code then added mul().

Output docs/assets/blame.gif

Require git
Require go

Set Shell "bash"
Set FontSize 20
Set Width 1000
Set Height 400
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 45ms

Hide
Type 'source "$(bash docs/assets/blame-setup.sh)" && clear'
Enter
Sleep 8s
Show

Type "# Ada wrote add(). Claude Code just added mul(). Commit as usual:"
Sleep 600ms
Enter
Type 'git commit -qam "Add mul"'
Sleep 400ms
Enter
Sleep 1.5s

Type "git attributed blame math.py"
Sleep 400ms
Enter
Sleep 3.5s

Type "git attributed verify"
Sleep 400ms
Enter
Sleep 4s
