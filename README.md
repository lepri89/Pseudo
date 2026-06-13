# Pseudo 🧠

> **The programming language that speaks your language.**

Pseudo is a new kind of programming language where you write **plain pseudocode** and it executes directly. No syntax to memorize. No boilerplate. No error messages you don't understand.

If you can explain what you want in plain English — you can program in Pseudo.

---

## Install

```bash
pip install pseudo-lang
```

---

## Run a program

```bash
pseudo myprogram.pseudo
```

## Interactive mode (REPL)

```bash
pseudo
```

---

## What it looks like

```pseudo
# Even or odd
number = 18
divide number by 2
if remainder = 0 → say "even"
else → say "odd"
```

```pseudo
# Filter a list
students = [45, 78, 32, 91, 50, 29, 61]
go in students
find each item where item >= 50
print each
```

```pseudo
# Login check
username = "admin"
password = "1234"
check if username = "admin" and password = "1234"
if yes → say "welcome"
else → say "access denied"
```

```pseudo
# Define a function
define greet with name
    say "Hello,"
    print name
done

run greet with "World"
```

---

## Language Reference

### Variables
```pseudo
name = "Lepri"
age = 21
scores = [85, 42, 91, 67, 38]
```

### Conditions
```pseudo
check if age >= 18
if yes → say "adult"
else → say "minor"

if age >= 18 → say "adult"
else → say "minor"
```

### Loops & Filtering
```pseudo
go in scores
find each item where item >= 50
print each

remove from scores where score < 50
print scores
```

### Math
```pseudo
divide number by 2
if remainder = 0 → say "even"

add all prices together
print total

compare numbers
print biggest
print smallest

increase score by 10
decrease lives by 1
```

### Counting
```pseudo
count characters in password
if count >= 8 → say "strong"

count words in sentence
print count
```

### Functions
```pseudo
define calculate with a, b
    increase a by b
    print a
done

run calculate with 10, 25
```

### Repeat
```pseudo
repeat 5 times
    say "hello"
done
```

### Range
```pseudo
start from 0
go up to 10
print each
```

### Input / Output
```pseudo
ask username "What is your name?"
save data to "output.txt"
load content from "notes.txt"
```

### Search
```pseudo
go in sentence and check if "error" exists
if yes → say "found it"
else → say "not there"

go in emails
check each email if "@" exists
```

### Append
```pseudo
append "new item" to mylist
```

### Timing
```pseudo
wait 2 seconds
```

---

## Philosophy

> *Simple things should be simple. Complex things should be possible.*

Programming languages were designed for computers to understand. Pseudo is designed for **humans** to understand.

The idea: if you can explain what you want to a smart person in plain words — you can write it in Pseudo.

---

## Examples

See the `/examples` folder:
- `even_or_odd.pseudo`
- `login.pseudo`
- `students.pseudo`
- `functions.pseudo`
- `showcase.pseudo`

---

## Created by

**Emrin Demiri (Lepri)**  
Computer Engineering Student, IBU Skopje  
Co-founder of ReachPro

---

## License

MIT — free to use, modify, and distribute.

---

*"Linus built Linux with one laptop. This started the same way."*
