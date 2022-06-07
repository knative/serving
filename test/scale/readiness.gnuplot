set datafile separator comma
set key autotitle columnhead

set terminal pngcairo enhanced size 1600,800 font 'Helvetica,10'
set output filename.'.png'

# filename is a var that must be supplied with a -e "filename='value'"
set title filename." readiness timing" font 'Helvetica,15'
set ylabel 'Time to Ready (s)'

set style line 100 lt 1 lc rgb "grey" lw 0.5 # linestyle for the grid
set grid ytics linestyle 100

set xtics rotate nomirror
set margin 5

plot filename using 5:xticlabels(6) with points pointtype 2 notitle

