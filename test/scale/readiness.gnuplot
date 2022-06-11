set datafile separator comma
set key autotitle columnhead

# set terminal pngcairo enhanced size 1600,800 font 'Helvetica,10'
set term svg mouse dynamic standalone enhanced size 1600,800 font 'Helvetica,10'
set output filename.'.svg'

# filename is a var that must be supplied with a -e "filename='value'"
set title filename." readiness timing" font 'Helvetica,15'
set ylabel 'Time to Ready (s)'

set style line 100 lt 1 lc rgb "grey" lw 0.5 # linestyle for the grid
set grid ytics linestyle 100

set xtics rotate nomirror
set margin 5

LabelText(String,Size) = sprintf("name: %s\ntime: %d", stringcolumn(String), column(Size))

plot filename using 5:xticlabels(6) with points pointtype 2 notitle, \
  '' using 0:5:(LabelText(6,5)) with labels hypertext point pointtype 2 lc rgb "purple" notitle

