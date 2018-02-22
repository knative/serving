'use strict';

const express = require('express');

// Constants
const PORT = 8080;
const HOST = '0.0.0.0';

// App
const app = express();
app.get('/', (req, res) => {
  res.send(process.env.MESSAGE);
});

// Health check for k8s readiness probe
app.get('/healthz', (req, res) => {
  res.sendStatus(200)
});

app.listen(PORT, HOST);
console.log(`Running on http://${HOST}:${PORT}`);
