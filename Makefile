all: arklittlejohn arkpython3

arklittlejohn: Makefile spec.go fsark.go littlejohn.json
	cp littlejohn.json config.json
	go build -o arklittlejohn

arkpython3: Makefile spec.go fsark.go python3.json
	cp python3.json config.json
	go build -o arkpython3
