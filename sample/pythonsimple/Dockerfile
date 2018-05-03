FROM python:3.7-rc

RUN mkdir /app
ADD requirements.txt /app
ADD main.py /app
WORKDIR /app
RUN pip install -r requirements.txt
ENTRYPOINT ["python"]
CMD ["main.py"]
