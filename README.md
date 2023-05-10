A hacky start to both:

* Doing something Ark like
* Avoiding the broken state of GDAL's pip brokenness

If you run the Makefile you'll get two programs: arklittlejohn and arkpython3, which will look for a container image in a currently hardwired location, and run littlejohn or python3 in there, which means the persistence-calculator/H3Calculator should run without you needing to build a virtualenv, which is currently problematic.

Lots broken, but this is an MVP just to get the persistence pipeline back to a sane state, I'll tidy/fix things later.