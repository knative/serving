exports.function = (req, res) => {
  if (req.query.name === undefined) {
    res.status(400).send('Please specify a name query parameter. Example ?name=Max');
  } else {
    console.log(req.query.name);
    res.status(200).send('Hello ' + req.query.name);
  }
};